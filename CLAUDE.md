# CLAUDE.md

## Project Overview

gofile 是一个轻量级自建网盘服务：Go + Gin + GORM/MySQL + MinIO + Redis + Typesense + Vue 3。
支持文件上传/下载、分片上传断点续传、秒传去重、预签名直传直下、回收站、文件分享，以及 AI 语义检索（摘要/标签/自然语言搜索）。

## Tech Stack

- **Language:** Go 1.25.0
- **HTTP:** Gin 1.12
- **Database:** GORM 1.31 + MySQL（`golang-migrate` 版本化迁移；连接池 25 打开 / 10 空闲 / 5min 生命周期）
- **Storage:** `storage.Storage` 接口 — MinIO (S3) 优先，失败自动 fallback 本地磁盘（原子写）
- **Cache:** go-redis v9（可选）— 秒传缓存 / 分布式锁 / 全局限流；不可用时自动降级内存实现
- **Search:** Typesense（可选）— 全文 + 向量混合检索（RRF 融合）；不可用降级 MySQL LIKE
- **AI:** Provider 抽象（mock / openai / anthropic）+ 异步 worker pool；支持用户级自定义 OpenAI 协议 baseURL + API key
- **Auth:** bcrypt + HttpOnly Cookie Session（token 存 MySQL `tbl_user_token`，24h 过期）
- **Observability:** Prometheus 指标 + `log/slog` 结构化日志（request_id 串联）
- **Frontend:** Vue 3 + Vite + TypeScript（`web/`，构建产物 `web/dist`）
- **Docs:** Swagger 注解（`docs/` 生成物）、`BENCHMARKS.md` 基准报告

## Project Structure

```
gofile/
├── main.go                入口、依赖组装、路由注册、优雅关闭、Swagger 注解
├── config/config.go       环境变量配置（Server/MySQL/MinIO/Redis/AI/Cookie/Asynq）
├── model/                 GORM 模型 + DTO：File/UserFile/User/Token/AITask/AIConfig/Share/Multipart
├── repository/            数据访问层：接口 + GORM 实现 + 内存 mock（file/user/token/share/ai_task/ai_config/multipart）
├── service/               业务层：file/user/auth/share/ai/ai_config
├── handler/
│   ├── handler.go         上传/秒传/下载(Range)/预览/回收站/分片/预签名/VFS/S3 Multipart
│   ├── user.go            注册/登录/登出/用户信息
│   ├── share.go           分享创建/列表/撤销/免登录下载
│   ├── ai.go              语义检索（搜索/相似/重复检测）
│   ├── ai_config.go       用户级 AI Provider 配置
│   ├── auth.go            AuthMiddleware（Cookie session 验证）
│   ├── ratelimit.go       IP 限流（Redis Lua 固定窗口 / 内存令牌桶回退）
│   ├── cleanup.go         后台任务：chunk 清理、分享清理、AI 补偿、AI 任务 TTL、软删除 GC
│   └── errcode.go         统一业务错误码 + respondError
├── ai/
│   ├── provider.go        Provider 接口（Analyze/Embed/Dimension）
│   ├── factory.go         mock | openai | anthropic 工厂
│   ├── openai.go / anthropic.go / mock.go
│   ├── extract.go / pdf.go   文本提取（text/pdf/office/压缩包，1MB 预算）
│   ├── nlp.go             对话式查询解析（时间/类型/停用词）
│   ├── typesense.go / indexer.go / indexer_mock.go   检索引擎
│   └── processor.go       异步任务编排（worker pool + 状态机 + 补偿；Asynq 优先，chan 降级）
├── task/                  M3 Asynq 分布式任务调度（hibiken/asynq）
│   ├── types.go           任务类型常量 + Payload 结构体
│   ├── client.go          生产者：Enqueue（实现 ai.TaskEnqueuer，幂等 TaskID）
│   ├── server.go          消费者：NewServer（ai 队列权重 6，slog 桥接）
│   └── processor.go       Asynq handler → ai.Processor.ProcessOne
├── storage/
│   ├── storage.go         Storage 接口（Put/Get/GetRange/FileSize/Exists/Delete/PresignPut/PresignGet/InitMultipart/PresignPartPut/CompleteMultipart/AbortMultipart）
│   ├── minio.go           MinIO 实现（自动建桶 + 预签名 + S3 Multipart）
│   └── local.go           本地磁盘实现（原子写；Multipart 返回 ErrPresignNotSupported）
├── cache/                 Redis 封装：hash 秒传缓存 + 分布式锁
├── metrics/               Prometheus 指标 + request_id + 访问日志中间件
├── util/                  SHA1/MD5、分片追踪、AES-GCM 加解密、SSRF URL 校验
├── db/mysql/              GORM 连接池 + 启动时自动跑 migrations/
├── migrations/            golang-migrate 版本化迁移 SQL（000001_init, 000002_multipart_and_vfs）
├── docs/                  Swagger 生成文档（docs.go/swagger.json/yaml）
├── web/                   Vue 3 前端工程（Vite + TS + Vitest + ESLint）
├── schema.sql             建表脚本（与 migrations/ 同步，初始化参考）
├── .github/workflows/ci.yml  CI：gofmt → vet → test -race → build + 前端构建 + Docker
├── BENCHMARKS.md          基准测试报告
├── README.md / README_CN.md
└── AGENTS.md              AI 开发协作文档（与本文档保持同步）
```

## Build & Run

```bash
# 1) 前端构建（产物 web/dist，服务 /static 依赖它；未构建时首页 404）
cd web && npm ci && npm run build && cd ..

# 2) 数据库：只需创建空库 gofile，表结构由启动时自动迁移（migrations/）创建
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS gofile CHARACTER SET utf8mb4;"

# 3) 启动
cp .env.example .env   # 按需修改
go build -o gofile .
./gofile               # Windows: gofile.exe / start.bat

# Docker
docker compose up -d
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?...` | MySQL DSN（必填，启动自动迁移） |
| `UPLOAD_DIR` | `./uploads` | Local storage directory (fallback) |
| `CHUNK_DIR` | `./chunks` | Chunk temp directory |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO endpoint (empty = skip MinIO) |
| `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` | `minioadmin` | MinIO credentials |
| `MINIO_BUCKET` | `filestore` | MinIO bucket (auto-created) |
| `MINIO_USE_SSL` | `false` | Enable SSL for MinIO |
| `COOKIE_SECURE` | `false` | Cookie 仅 HTTPS 传输（生产置 true） |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / 空 / 0 | Redis（不可用自动降级） |
| `AI_ENABLED` | `false` | AI 功能总开关 |
| `AI_PROVIDER` | `mock` | `mock` \| `openai` \| `anthropic` |
| `AI_API_KEY` | 空 | LLM API Key（mock 忽略） |
| `AI_MODEL` | 空 | LLM 模型名（空用 provider 默认） |
| `AI_EMBED_DIM` | `128` | 向量维度（Typesense collection 创建后不可改） |
| `AI_WORKERS` | `4` | 异步 worker 数量 |
| `TYPESENSE_URL` / `TYPESENSE_API_KEY` | `http://localhost:8108` / `xyz` | 检索引擎（不可用降级 LIKE） |
| `AI_CONFIG_SECRET` | 空 | 用户 API key 加密密钥（未配置从 DSN 派生） |
| `ALLOW_PRIVATE_AI_URL` | `false` | 允许自定义 baseURL 指向内网（本地 Ollama 场景） |
| `ASYNQ_ENABLED` | `false` | 启用 Asynq 持久化任务队列（替代进程内 chan，需 Redis）；true = 任务写 Redis，跨实例，内置重试 |

> 所有可选依赖（Redis/AI/Typesense/Asynq）缺失时服务照常启动并降级。

## API Endpoints

### 文件（`/file` 组，全部需要鉴权）
| Method | Route | Description |
|--------|-------|-------------|
| POST | `/file/upload` | 上传（含秒传检测，100MB 上限） |
| GET | `/file/meta` | 文件元信息（含 AI 摘要/标签） |
| GET | `/file/query` | 文件列表（支持 `parent_id` 目录 + `pa### 系统
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/healthz` | 健康检查 |
| GET | `/metrics` | Prometheus 指标 |
| GET | `/static/*` | 前端静态资源（`web/dist`） |
| GET | `/` | 重定向到 `/static/index.html` |

## Architecture Notes

### 三层架构与依赖注入
`handler → service → repository + storage/cache/ai/task` 单向依赖，全部通过构造器注入（`NewXxxHandler(svc, cfg)`），`main.go` 负责组装。可选依赖（Redis/AI/Typesense/Asynq）以 nil 安全方式注入，缺失时自动降级。

### 响应格式与统一错误码
- 统一响应：`gin.H{"code": 0|错误码, "msg": "...", "data": ...}`，成功 code=0
- handler 统一使用 `respondError(c, httpStatus, code, msg)`（`handler/errcode.go`）

| 码 | 常量 | 含义 |
|----|------|------|
| 0 | CodeOK | 成功 |
| 1001 | CodeInvalidParams | 参数缺失/格式错误 |
| 1002 | CodeUnauthorized | 未登录/登录失效 |
| 1003 | CodeForbidden | 越权 |
| 1004 | CodeNotFound | 不存在 |
| 1005 | CodeUserExists | 用户名已存在 |
| 1006 | CodeInvalidCreds | 用户名或密码错误 |
| 1007 | CodeUploadFailed | 上传失败 |
| 1008 | CodeMergeFailed | 分片合并失败 |
| 1009 | CodeStorageError | 存储层错误（如预签名仅支持 MinIO） |
| 1010 | CodeTooManyRequests | 限流 |
| 1011 | CodeSearchFailed | AI 检索失败 |
| 1099 | CodeInternalError | 兜底 |

### Auth Flow
1. signup：bcrypt 哈希 → `tbl_user`（用户名主键，重复返回 1005）
2. signin：bcrypt 验证 → `crypto/rand` 生成 64 位 hex token → `tbl_user_token`（24h 过期）
3. `Set-Cookie`：HttpOnly + SameSite=Lax + Secure（`COOKIE_SECURE`），1h 有效期
4. `AuthMiddleware`：Cookie username+token → `tokenRepo.Get` → 比对 + 过期校验，失败 1002
5. logout：删除服务端 token + 清除 Cookie（幂等）
6. Token **永不**出现在 JSON 响应体，仅通过 Cookie 传递

### 存储层
- `storage.Storage`：`Put / Get / GetRange / FileSize / Exists / Delete / PresignPut / PresignGet / InitMultipart / PresignPartPut / CompleteMultipart / AbortMultipart`
- MinIO 优先，失败 fallback 本地磁盘；本地 `Put` 为原子写（临时文件 + rename）
- 本地存储的 `PresignPut/PresignGet/Multipart` 返回 `ErrPresignNotSupported`
- MySQL 只存元数据；文件内容在存储层，key = `file_sha1`

### 文件所有权（全局去重 × 用户隔离）
- `tbl_file` 全局去重（`file_sha1` 主键），`tbl_user_file` 每用户每文件/文件夹一行
- 秒传 = 命中全局文件后仅为当前用户插入关联行并触发 AI 任务投递
- 下载/重命名/删除/移动/分享等所有操作先经 `fileRepo.GetByHash(filehash, username)` 校验所有权，无权返回 1003

### VFS 虚拟文件系统
- 物化路径（Materialized Path）：`dir_path` 字段记录完整路径（如 `/资料/Go/`），方便前缀检索任意子孙节点
- 文件夹操作：新建、重命名、移动（含防循环嵌套校验：禁止移入自身子文件夹）、面包屑导航
- 递归更新：文件夹改名/移动时原子批量更新子孙节点物化路径前缀

### S3 Multipart 分片直传
- 针对大文件：`init` 签发各分片预签名 PUT URL → 客户端并发直接 PUT 到 MinIO → `complete` 存储层服务端合并
- 应用服务器实现零本地磁盘 I/O、零网络中转带宽开销

### 异步任务与调度（M3 Asynq）
- 双路任务队列：`ASYNQ_ENABLED=true` 且 Redis 可用时走 `task.Client`（Redis 持久化、跨实例调度、MaxRetry=3 指数退避、死信队列）；Redis 故障或未开启时自动降级进程内 chan（容量 100）
- 任务幂等：TaskID = `username:filehash`，`tbl_ai_task` 状态机双保险（0 待处理 / 1 处理中 / 2 完成 / 3 失败）

### 回收站与 GC
- 软删除 `status=2`（存储层不动）；`/trash` 分页列表；`/restore` 恢复（并重入队 AI 重建索引）；`/purge` 彻底删除
- purge：删除关联行后引用数为 0 时同步清理存储层 + `tbl_file` + 检索引擎
- 后台 GC：创建超 7 天且 0 活跃引用的文件，24h 扫描一次，从存储层移除并清理 Typesense 文档

### 文件分享
- `tbl_share`：64 位 hex 令牌（crypto/rand，防枚举）+ 可选 bcrypt 提取码 + 有效期 1-30 天（默认 7）
- 免登录下载 `GET /share/:token`（限流 10/s burst 20 防提取码爆破），支持 Range 断点
- 解析时校验：过期、提取码、文件仍存在（以分享者身份校验所有权）；密码哈希永不下发（`HasPassword` 仅标记）
- 每日清理过期分享

### 限流
- `/user/signup`、`/user/signin`：5 req/s burst 10（`c.ClientIP()`）
- Redis 可用：Lua 固定窗口（`gofile:ratelimit:<ip>`，多实例共享）；否则内存令牌桶（空闲 IP 5min 回收）

### AI 异步管线与语义检索（`AI_ENABLED=true`）
- `ai.Provider` 接口：`Analyze / Embed / Dimension`；`factory.go` 按 `AI_PROVIDER` 选择 mock|openai|anthropic
- 管线：extract（文本/PDF/Office/压缩包，1MB 读取预算）→ analyze（LLM 摘要+标签）→ embed → `SaveAnalysis` → Typesense upsert
- 检索：`/file/ai/search?q=`（NLP 自然语言解析 + 混合检索 RRF）、`/file/ai/similar`、`/file/ai/duplicates`
- 用户级配置：`/ai/config` CRUD + `/test` 连通性测试（AES-GCM 加密落库，防 SSRF）

### 可观测性
- 中间件顺序（不变量，勿调换）：`RequestIDMiddleware → MetricsMiddleware → gin.Recovery`
- 指标：`http_requests_total`（method/path/status）、`http_request_duration_seconds`、`file_upload_bytes_total`、`ai_tasks_total`、`ai_llm_duration_seconds`、`ai_index_ops_total`
- `/metrics` 端点（promhttp 默认注册表，含 go 运行时指标）；request_id 由 `ContextHandler` 自动附加到所有 slog 日志

## Testing

```bash
go test ./...           # 全量（无需外部依赖，全部绿）
go test -race ./...     # 竞态检测（CI 执行）
go test ./util/ -bench . -benchmem -run '^$'   # 基准（BENCHMARKS.md）
```

- repository 层测试用内存 SQLite（`glebarez/sqlite`），不依赖 MySQL
- 测试覆盖：`ai/` `handler/` `metrics/` `repository/` `service/` `storage/` `util/`

## Development Conventions

- 注释中英双语；导出标识符 PascalCase、未导出 camelCase
- 错误处理：I/O 操作必查 error；DB 写失败需回滚存储层（`store.Delete(ctx, key)`）；`slog` 结构化日志带 context（自动附加 request_id）
- handler 统一用 `respondError` 返回业务错误码，前端只判断 `code === 0`
- 测试用 `repository` 包内 `NewMockXxxRepository` + `httptest`，不依赖真实 MySQL/Redis
- 所有 handler 通过构造器注入依赖，禁止包级全局状态
- API 前缀：`/file`（鉴权）、`/user`、`/share`（公开限流）、`/ai/config`（鉴权）

## 已知问题

- ~~🔴 `handler/handler.go` FastUpload 缺 enqueue~~ ✅ **已修复**（`50d7309`）：`FastUpload()` 末尾补 `s.enqueue()`，秒传用户 AI 任务现在正确投递
- 🟡 `web/dist` 未入库：本地/CI 需先 `cd web && npm run build`，否则 `/static` 404。
- 🟡 Range 开放区间（`bytes=N-`）在未知文件大小时返回 416。
- 🟡 分页为 `LIMIT/OFFSET`，大数据量深分页需改游标分页。

## Roadmap 状态

**已落地**：
- P0 五项 Bug 修复
- P1 Redis（秒传缓存/分布式锁/全局限流）
- P2 预签名直传直下
- P3 Range + 分页
- P4 可观测性（Prometheus + request_id + Grafana dashboard）
- P6 AI（摘要/标签/语义搜索/相似/重复检测/用户级配置）
- **M1** S3 Multipart 分片直传直合（`c9d150f`，2026-08-16）
- **M2** VFS 虚拟文件系统（物化路径、无限层级目录、面包屑）（`c9d150f`，2026-08-16）
- **M3** Asynq 分布式任务调度（`50d7309`，2026-08-16）：task/ 包，双路 Enqueue，ProcessOne 公开方法
- 工程化：CI（gofmt+vet+test-race+build+docker）、Swagger、golang-migrate、BENCHMARKS、repository 测试（内存 SQLite）

**待办**：
- P5 剩余：VFS/Multipart/task 专项测试，覆盖率目标 ≥80%；MinIO 并发压测
- M4 RAG 知识库问答（Chat with your Drive）：文档分块 + SSE 流式输出
- M5 WebDAV 协议支持：golang.org/x/net/webdav，Windows/Mac 挂载
- P7 K8s 部署：Helm Chart + HPA + ConfigMap
