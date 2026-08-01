# CLI.md

> AI 开发助手协作文档 — 与 CLAUDE.md 保持同步

## 项目概览

gofile 是一个轻量级网盘服务，Go 语言编写，Gin 框架 + MySQL + MinIO。支持文件上传下载、分片上传断点续传、用户认证、秒传去重。

## 快速启动

```bash
# 数据库准备（首次）
mysql -u root -p gofile < schema.sql

# 启动
go run main.go

# 或使用脚本
./scripts/start.sh --migrate
```

## 代码导航

### 入口与路由
- `main.go` — 注册所有路由，初始化存储层，启动优雅关闭

### 文件处理（handler/handler.go）
- 所有 handler 使用全局变量 `globalStore`(storage.Storage) 和 `globalCfg`(*config.Config)
- 响应格式统一: `gin.H{"code": 0|1, "msg": "...", "data": ...}`
- 所有 `/file/*` 路由都经过 `AuthMiddleware` 鉴权

### 用户认证（handler/user.go + auth.go）
- `SignupHandler` — bcrypt 哈希密码，写入 `tbl_user`
- `SignInHandler` — bcrypt 验证，生成 64 位 hex token，写入 Cookie
- `AuthMiddleware` — 从 Cookie 读取 `username` + `token`，验证 `tbl_user_token`
- `checkPassword` / `isTokenValid` — 直接 `QueryRow` 操作 MySQL

### 数据库操作
- `db/mysql/conn.go` — 连接池（Init/DBConn）
- `db/file.go` — `tbl_file` 操作（所有查询按 `status=1` 过滤，按 `user_name` 隔离）
- `db/user.go` — `tbl_user` / `tbl_user_token` 操作
- `meta/filemeta.go` — `FileMeta` 结构体，`toFileMeta()` / `toFileMetas()` 辅助转换

### 存储抽象
- `storage.Storage` 接口: `Put(ctx, key, reader, size) error` / `Get(ctx, key) (ReadCloser, error)` / `Exists(ctx, key) (bool, error)` / `Delete(ctx, key) error`
- `storage/minio.go` — MinIO 实现
- `storage/local.go` — 本地文件系统实现

### 中间件
- `handler/auth.go` — `AuthMiddleware()`: Cookie session 验证
- `handler/ratelimit.go` — `RateLimitMiddleware(rate, burst)`: IP 令牌桶限流
- `handler/cleanup.go` — `StartChunkCleanup()`: 定时清理过期 chunk

## 关键约定

### 文件所有权
- 所有文件操作必须验证用户权限: `meta.GetFileMetaDBByUser(fileSha1, username)`
- 返回 403 当用户无权操作
- 文件列表只返回当前用户文件: `meta.GetAllFileMetaDBByUser(username)`

### 错误处理
- `io.Copy` / `file.Seek` 等 I/O 操作必须检查 error
- 数据库写入失败时需回滚存储层: `globalStore.Delete(ctx, key)`
- 使用 `slog.Error` / `slog.Warn` 记录错误上下文

### 测试
- handler 测试使用 `httptest.NewRecorder` + `gin.Context`
- 无 MySQL 环境时，预期 panic 并 recover（`gin.Recovery()`）
- 新增 handler 需要添加对应的测试

### 安全
- 用户输入使用 `filepath.Base()` 防止路径穿越
- 上传大小限制: `MaxUploadSize = 100 << 20` (100MB)
- Cookie 设置 `HttpOnly` 防止 XSS 窃取
- 密码使用 bcrypt 哈希，不存储明文

## 常见任务

### 添加新 API
1. 在 `handler/handler.go` 或新文件实现 handler 函数
2. 在 `main.go` 对应路由组注册路由
3. 添加测试文件覆盖 handler
4. 更新 `README.md` 和 `CLAUDE.md` 的 API 表格

### 修改数据库表
1. 更新 `schema.sql`（建表脚本）
2. 更新 `db/` 对应文件的 CRUD 函数
3. 表结构变更需同步更新 `meta/` 的转换函数

### 添加新存储后端
1. 实现 `storage.Storage` 接口
2. 在 `main.go` 中添加初始化逻辑和 fallback 策略
3. 添加对应的测试