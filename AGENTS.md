# AGENTS.md

> AI 开发助手协作文档 — 与 CLAUDE.md 保持同步

## 项目概览

gofile 是一个轻量级自建网盘服务：Go + Gin + GORM/MySQL + MinIO + Redis + Typesense + Vue 3。
支持文件上传/下载、分片上传断点续传、秒传去重、预签名直传直下、回收站、文件分享，以及 AI 语义检索（摘要/标签/自然语言搜索）。

## 快速启动

```bash
# 1) 前端构建（产物 web/dist，服务 /static 依赖它；未构建时首页 404）
cd web && npm ci && npm run build && cd ..

# 2) 数据库：只需创建空库 gofile，表结构由启动时自动迁移（migrations/）创建
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS gofile CHARACTER SET utf8mb4;"

# 3) 启动
cp .env.example .env
go run main.go        # 或 go build -o gofile . && ./gofile
```

## 代码导航

### 入口与路由（main.go）
- 依赖组装：`repository → service → handler` 构造器注入，禁止包级全局状态
- 中间件顺序（不变量，勿调换）：`RequestIDMiddleware → MetricsMiddleware → gin.Recovery`
- 可选依赖（Redis/AI/Typesense）以 nil 安全方式注入，缺失自动降级
- 路由分组：`/file`（鉴权）、`/user`、`/share/:token`（公开限流）、`/ai/config`（鉴权，AI_ENABLED 时注册）

### 响应约定（handler/errcode.go）
- 统一响应：`gin.H{"code": 0|错误码, "msg": "...", "data": ...}`，成功 code=0
- handler 统一用 `respondError(c, httpStatus, code, msg)`；前端只判断 `code === 0`
- 错误码：1001 参数 / 1002 未登录 / 1003 越权 / 1004 不存在 / 1005 用户已存在 / 1006 凭据错误 / 1007 上传 / 1008 合并 / 1009 存储 / 1010 限流 / 1011 检索 / 1099 兜底

### 用户认证（handler/user.go + auth.go + service/user_service.go + auth_service.go）
- `SignupHandler` — bcrypt 哈希 → `tbl_user`（用户名主键，重复返回 1005）
- `SignInHandler` — bcrypt 验证 → 64 位 hex token（crypto/rand）→ `tbl_user_token`（24h 过期）→ HttpOnly Cookie（1h + SameSite=Lax + Secure 可配）
- `LogoutHandler` — 删服务端 token + 清 Cookie（幂等）
- `AuthMiddleware` — Cookie username+token → `tokenRepo.Get` 比对 + 过期校验，失败 1002

### 文件处理（handler/handler.go + service/file_service.go）
- 上传链路：流式 SHA1 → Redis 秒传缓存命中 → 存储层确认 → `tbl_file` 全局注册（INSERT IGNORE 幂等）→ `tbl_user_file` 关联行 → 异步 AI 分析
- 所有权模型：`tbl_file` 全局去重 × `tbl_user_file` 每用户一行（UNIQUE(user_name, file_sha1)）；秒传为当前用户插入关联行
- 所有操作先经 `fileRepo.GetByHash(filehash, username)` 校验所有权，无权返回 1003
- 下载支持 HTTP Range（206）；预览按扩展名 + 文件头探测 MIME
- 回收站：软删除 status=2，`/trash` 分页、`/restore` 恢复（重入队 AI）、`/purge` 彻底删除（零引用时清存储层 + 索引）
- 分片上传：目录 `<CHUNK_DIR>/<username>/<filehash>/<index>`（用户隔离），merge 用 Redis 分布式锁 + UUID 临时文件
- 预签名：`PresignUpload`（15min）→ 前端直传 MinIO → `ConfirmUpload`；`PresignDownload`（5min）；本地存储返回 `ErrPresignNotSupported`

### 文件分享（handler/share.go + service/share_service.go）
- `Create`：校验所有权 → 64 位 hex 令牌 + 可选 bcrypt 提取码 + 有效期 1-30 天（默认 7）
- `Resolve`：校验过期/提取码/文件仍存在（以分享者身份）；密码哈希永不下发
- 免登录下载 `GET /share/:token?pwd=`（限流 10/s burst 20，支持 Range）

### 数据访问（repository/）
- 接口 + GORM 实现 + 内存 mock（`NewMockXxxRepository` 供测试），所有方法带 ctx
- 文件：Create / CreateUserFile / GetByHash / ListByUser / CountByUser / ListByUserPaged / ListTrash / Restore / PurgeUserFile / Delete / UpdateName / CountRefs / ListOldest / RemoveOrphan / SaveAnalysis / GetGlobalFile
- 用户：Create / GetPasswordHash / GetInfo；Token：Upsert / Get / Delete
- 分享：CreateShare / GetShareByToken / ListShares / DeleteShare / DeleteExpired
- AI 任务：CreateTask / GetTask / MarkProcessing / MarkDone / MarkFailed / ListRequeueable / CleanupExpired
- AI 配置：Get / Upsert / Delete

### 存储抽象（storage/）
- 接口：`Put / Get / GetRange / FileSize / Exists / Delete / PresignPut / PresignGet`
- MinIO 优先，失败 fallback 本地磁盘（原子写：临时文件 + rename）；key = file_sha1

### 缓存（cache/）
- 秒传哈希缓存（MarkHash/HashExists）+ 分布式锁（AcquireLock/ReleaseLock，SETNX + Lua CAS）
- 限流：Redis Lua 固定窗口 / 内存令牌桶回退（handler/ratelimit.go）

### 后台任务（handler/cleanup.go）
- chunk 清理（24h 过期，1h 扫描）、分享清理（每日）、AI 补偿（1m→30m 退避）、AI 任务 TTL（7 天，每日）、软删除 GC（7 天孤儿，24h 扫描，含索引清理）

### AI 能力（ai/ + handler/ai.go + ai_config.go）
- Provider 接口：`Analyze / Embed / Dimension`；factory.go 按 `AI_PROVIDER` 选 mock|openai|anthropic
- Processor 异步管线：extract → analyze → embed → SaveAnalysis → Typesense upsert；秒传命中跳过 LLM；失败 retry ≤3
- 检索：`/file/ai/search`（NLP 解析 + 混合检索 RRF）、`/file/ai/similar`、`/file/ai/duplicates`；Typesense 不可用降级 MySQL LIKE
- 用户级配置：`/ai/config` CRUD + `/test`；API key AES-GCM 加密落库只回掩码；baseURL 防 SSRF（`ALLOW_PRIVATE_AI_URL` 放行内网）

### 可观测性（metrics/）
- 指标：http_requests_total / http_request_duration_seconds / file_upload_bytes_total / ai_tasks_total / ai_llm_duration_seconds / ai_index_ops_total
- request_id 由 ContextHandler 自动附加到所有 slog 日志；`/metrics` 端点

### 工具函数（util/）
- hash.go：SHA1/MD5 流式计算；chunk.go：分片追踪（用户隔离）
- crypto.go：AES-GCM 加解密（AI API key）；urlcheck.go：SSRF URL 校验

## 关键约定

### 文件所有权
- 所有文件操作必须验证：`fileRepo.GetByHash(filehash, username)`，无权返回 1003
- 文件列表/回收站只返回当前用户数据（`ListByUser`/`ListTrash`）

### 错误处理
- `io.Copy` / `file.Seek` 等 I/O 操作必须检查 error
- 数据库写入失败需回滚存储层：`store.Delete(ctx, key)`
- 使用 `slog.InfoContext/WarnContext/ErrorContext` 记录上下文（自动附加 request_id）

### 测试
- 使用 `repository.NewMockXxxRepository` + `httptest`，不依赖真实 MySQL/Redis
- `go test ./...` 全绿；CI：gofmt → vet → test -race → build
- 新增 handler/service/repository 需要添加对应测试

### 安全
- 用户输入用 `filepath.Base()` 防路径穿越；filehash 必须 40 位 hex 校验（`isValidHash`）
- 上传大小限制：`MaxUploadSize = 100 << 20`（100MB）
- Cookie：HttpOnly + SameSite=Lax + Secure（`COOKIE_SECURE`，生产置 true）
- 密码/提取码 bcrypt 哈希；API key AES-GCM 加密落库；自定义 URL 防 SSRF

## 常见任务

### 添加新 API
1. service 层实现业务方法（含所有权校验）
2. handler 层新增方法（`respondError` 统一错误码）+ Swagger 注解
3. `main.go` 注册路由（注意中间件：鉴权/限流）
4. 添加测试（mock repository + httptest）
5. 更新 README/README_CN API 表格，同步 docs/（swag 重新生成）

### 修改数据库表
1. `migrations/` 新增版本化迁移（`.up.sql` + `.down.sql`，勿改已发布迁移）
2. `model/` 同步 GORM 模型
3. `repository/` 对应 CRUD（全部带 ctx）
4. `schema.sql` 同步（初始化参考）

### 添加新存储后端
1. 实现 `storage.Storage` 全接口（含 `GetRange/FileSize/PresignPut/PresignGet`）
2. `main.go` 初始化 + fallback 策略
3. 添加对应测试

### 添加新 AI Provider
1. 实现 `ai.Provider`（`Analyze/Embed/Dimension`）
2. `ai/factory.go` 注册
3. 如需用户自定义，在 `AIConfigService.ResolveProvider` 中接入

### 前端
- 开发：`cd web && npm run dev`；构建：`npm run build`（产物 `web/dist`）；测试：`npm test`

## 已知问题

- 🔴 `handler/handler.go` UploadHandler 的 FastUpload 短路分支不创建 `tbl_user_file` 关联行 → 秒传用户看不到文件。修复应移除 handler 短路或改为走 service.Upload。
- 🟡 `web/dist` 未入库：本地/CI 需先 `npm run build`，否则 `/static` 404。
- 🟡 Range 开放区间（`bytes=N-`）在未知文件大小时返回 416；分页为 LIMIT/OFFSET，大数据量需改游标。
