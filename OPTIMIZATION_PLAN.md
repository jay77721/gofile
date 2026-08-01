# gofile 优化 Plan

> 基于代码审查和功能分析，识别出的优化点与改进方案。分三期实施，按优先级递减。

---

## 一期：数据一致性修复（高优先级）

### 1.1 MySQL 写入失败时回滚磁盘文件

**问题**：`UploadHandler` 和 `MergeChunkHandler` 中，`store.Put()` 成功后 `meta.UpdateFileMetaDB()` 失败，磁盘文件已写入但无元数据记录，成为孤儿文件，永久占用存储且无法通过 API 管理。

**方案**：MySQL 失败时删除已写入的磁盘文件。

```go
// UploadHandler 改进后
if ok := meta.UpdateFileMetaDB(fileMeta); !ok {
    // 回滚：删除已上传的存储文件
    _ = store.Delete(context.Background(), fileSha1)
    slog.Warn("save file meta failed, rolled back storage", "filehash", fileMeta.FileSha1)
    c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "文件上传失败", "data": nil})
    return
}
```

**影响文件**：`handler/handler.go` — `UploadHandler`, `MergeChunkHandler`

---

### 1.2 Chunk 分片设置 TTL 自动过期

**问题**：分片上传中断后，`./chunks/<hash>/` 目录和 Redis `chunk:<hash>` key 永远残留，造成磁盘和内存泄漏。

**方案**：
- Redis key 设置 24h 过期
- 新增定时任务清理超时的磁盘 chunk 目录

```go
// util/chunk.go - AddChunk 增加 TTL
func AddChunk(filehash string, index int) error {
    key := "chunk:" + filehash
    return rd.RDB.SAdd(rd.Ctx, key, index).Err()
    // 调用后设置过期：rd.RDB.Expire(rd.Ctx, key, 24*time.Hour)
}
```

```go
// 新增 cleanup 定时任务（handler/handler.go 或独立文件）
func StartChunkCleanup(interval time.Duration, chunkDir string) {
    go func() {
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        for range ticker.C {
            entries, _ := os.ReadDir(chunkDir)
            for _, entry := range entries {
                info, _ := entry.Info()
                if time.Since(info.ModTime()) > 24*time.Hour {
                    os.RemoveAll(filepath.Join(chunkDir, entry.Name()))
                }
            }
        }
    }()
}
```

**影响文件**：`util/chunk.go`，`handler/handler.go`（或新建 `handler/cleanup.go`）

---

### 1.3 统一错误处理，消除 `_ =` 吞错

**问题**：多处使用 `_ = err` 忽略错误（`AddChunk`、`ClearChunks`、`store.Delete`），导致失败时无感知。

**方案**：所有错误必须记录日志，关键错误必须返回。

```go
// 改进前
_ = util.AddChunk(fileHash, chunkIndex)

// 改进后
if err := util.AddChunk(fileHash, chunkIndex); err != nil {
    slog.Error("add chunk index failed", "error", err, "filehash", fileHash, "index", chunkIndex)
}
```

**影响文件**：`handler/handler.go` 全部 handler

---

## 二期：功能完善（中优先级）

### 2.1 移除 Redis 依赖

**问题**：Redis 在项目中无实际作用：
- `SetFileHash` 写了但没有任何读取逻辑
- `chunk:<hash>` 索引只是优化，磁盘上的 chunk 文件才是真正的数据
- 增加运维成本和故障点

**方案**：
- 删除 `rd/` 目录
- 删除 `util/chunk.go`（逻辑移到 handler 或改为磁盘-based）
- 分片状态改为读取磁盘目录判断

```go
// 替代 ChunkExists：检查磁盘文件是否存在
func ChunkExists(filehash string, index int) bool {
    chunkPath := filepath.Join(cfg.ChunkDir, filehash, strconv.Itoa(index))
    _, err := os.Stat(chunkPath)
    return err == nil
}

// 替代 GetUploadedChunks：读取目录文件列表
func GetUploadedChunks(filehash string) ([]string, error) {
    dir := filepath.Join(cfg.ChunkDir, filehash)
    entries, err := os.ReadDir(dir)
    if err != nil { return nil, err }
    var chunks []string
    for _, e := range entries { chunks = append(chunks, e.Name()) }
    return chunks, nil
}
```

**影响文件**：`rd/redis.go`，`util/chunk.go`，`handler/handler.go`，`main.go`，`go.mod`

---

### 2.2 下载鉴权：校验文件所有权

**问题**：`DownloadHandler` 只检查登录状态，不检查文件归属。任何登录用户只要知道 filehash 就能下载他人文件。

**方案**：文件元数据表增加 `username` 字段，下载时校验。

```go
// meta/filemeta.go - FileMeta 增加 Owner 字段
type FileMeta struct {
    FileSha1 string
    FileName string
    FileSize int64
    Location string
    UploadAt time.Time
    Username string  // 新增：文件所有者
}

// handler/handler.go - DownloadHandler 增加归属校验
func DownloadHandler(c *gin.Context) {
    // ... 现有逻辑 ...
    username, _ := c.Cookie("username")
    if fMeta.Username != username {
        c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权访问该文件", "data": nil})
        return
    }
}
```

**影响文件**：`meta/filemeta.go`，`db/file.go`，`handler/handler.go`，`migrations/`

---

### 2.3 修复限流 IP 伪造问题

**问题**：限流直接用 `c.ClientIP()`，在有反向代理场景下可被 `X-Forwarded-For` 头部绕过。

**方案**：增加可信代理配置，从右向左遍历取第一个不可信 IP。

```go
// handler/ratelimit.go - 增加 IP 获取逻辑
func getClientIP(c *gin.Context) string {
    // 如果 Gin 设置了 TrustedProxies，ClientIP() 已经处理了
    // 额外防护：限制 burst 内的 IP 数量
    return c.ClientIP()
}
```

**说明**：Gin 的 `c.ClientIP()` 在正确配置 `SetTrustedProxies` 后已经安全。当前主要问题是 `main.go` 没有设置 `TrustedProxies`。

**影响文件**：`main.go`

---

## 三期：可维护性提升（低优先级）

### 3.1 拆分大函数

**问题**：`MergeChunkHandler`（120 行）、`UploadHandler`（86 行）过长，难以阅读和测试。

**方案**：提取子函数。

```go
// MergeChunkHandler 拆分为：
func validateChunkCount(chunkDir, fileHash, totalStr string) error { ... }
func mergeChunksToTemp(chunkDir, fileHash string) (string, int64, error) { ... }
func saveMergedFile(fileHash, fileName string, reader io.Reader, size int64) error { ... }
```

**影响文件**：`handler/handler.go`

---

### 3.2 消除全局可变变量

**问题**：`handler.go` 中 `var store`、`var cfg` 是包级全局变量，不利于单元测试。

**方案**：改为结构体依赖注入。

```go
type FileHandler struct {
    store storage.Storage
    cfg   *config.Config
}

func NewFileHandler(s storage.Storage, c *config.Config) *FileHandler {
    return &FileHandler{store: s, cfg: c}
}

func (h *FileHandler) Upload(c *gin.Context) { ... }
```

**影响文件**：`handler/handler.go`，`main.go`

---

### 3.3 补充单元测试

**问题**：`UploadHandler`、`UploadChunkHandler`、`OnFileUploadFinished` 无测试覆盖。

**方案**：
- 使用 `httptest` + `sqlmock` 编写 handler 测试
- 使用 `minio/minio-go` 的 mock 或临时目录测试 storage 层

**影响文件**：`handler/handler_test.go`

---

### 3.4 Token 返回冗余清理

**问题**：`SignInHandler` 同时在 Cookie 和 JSON body 中返回 token，JSON 中的 token 增加泄露面。

**方案**：只通过 HttpOnly Cookie 返回 token，JSON 中不再包含。

```go
// 改进前
c.JSON(200, gin.H{
    "data": gin.H{
        "Token": token,  // 删除这行
    },
})
```

**影响文件**：`handler/user.go`

---

## 实施顺序建议

```
一期（本周）
  ├─ 1.1 MySQL 回滚修复        ← 防止数据泄漏
  ├─ 1.2 Chunk TTL             ← 防止资源泄漏
  └─ 1.3 统一错误处理          ← 提升可观测性

二期（下周）
  ├─ 2.1 移除 Redis            ← 简化架构
  ├─ 2.2 下载鉴权              ← 安全修复
  └─ 2.3 限流配置修复          ← 安全加固

三期（按需）
  ├─ 3.1 函数拆分
  ├─ 3.2 依赖注入
  ├─ 3.3 补充测试
  └─ 3.4 Token 清理
```

---

## 风险与回滚

| 变更 | 风险 | 回滚策略 |
|------|------|----------|
| MySQL 回滚 | 存储层 Delete 可能失败 | 记录日志，异步清理任务兜底 |
| 移除 Redis | 分片状态丢失 | 改为磁盘-based，无需额外组件 |
| 下载鉴权 | 老数据无 Username 字段 | 迁移时默认空字符串，不拦截已有文件 |
| 依赖注入 | 改动面大 | 分步迁移，保留全局变量兼容 |
