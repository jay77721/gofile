# CLAUDE.md

## Project Overview

gofile 是一个轻量级网盘服务，Go 语言编写，Gin + MySQL + MinIO。支持文件上传/下载、分片上传断点续传、用户认证、秒传去重。

## Tech Stack

- **Language:** Go 1.25.0
- **HTTP:** Gin
- **Database:** MySQL via `go-sql-driver/mysql` (连接池, 默认 25 连接)
- **Storage:** `storage.Storage` 接口 — MinIO (S3) 优先, 失败自动 fallback 本地磁盘
- **Auth:** bcrypt + cookie session (token 存 MySQL `tbl_user_token`, 24h 过期)
- **Frontend:** Vanilla HTML + fetch API
- **Logging:** `log/slog` (结构化 JSON)
- **Container:** Docker + Docker Compose

## Project Structure

```
gofile/
├── main.go                入口、路由注册、优雅关闭
├── schema.sql             数据库建表脚本（含索引，单文件）
├── config/
│   └── config.go          环境变量配置
├── db/
│   ├── mysql/
│   │   └── conn.go        MySQL 连接池
│   ├── file.go            tbl_file CRUD + FileMeta 领域模型
│   └── user.go            tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go         文件上传/下载/查询/删除 + 分片上传 + 合并
│   ├── handler_test.go    handler 测试
│   ├── user.go            注册/登录/用户信息 + bcrypt
│   ├── user_test.go       用户 handler 测试
│   ├── auth.go            AuthMiddleware (Cookie session)
│   ├── ratelimit.go       IP 令牌桶限流 (5 req/s, burst 10)
│   └── cleanup.go         定时清理过期 chunk
├── storage/
│   ├── storage.go         Storage 接口定义
│   ├── minio.go           MinIO 对象存储实现
│   └── local.go           本地文件系统实现
├── util/
│   ├── hash.go            SHA1、MD5、文件哈希、路径工具
│   ├── hash_test.go       工具函数测试
│   └── chunk.go           磁盘-based 分片追踪
├── static/                前端 HTML 页面
├── start.sh               Unix/macOS 启动脚本
├── start.bat              Windows 启动脚本
├── .env.example            环境变量模板
├── Dockerfile             多阶段 Docker 构建
├── docker-compose.yml      Docker Compose 编排
├── README.md              项目说明文档 (EN)
├── README_CN.md           项目说明文档 (ZH)
└── AGENTS.md              AI 开发协作文档
```

## Build & Run

```bash
# Docker
docker compose up -d

# Manual
go build -o gofile .
cp .env.example .env
./gofile

# Scripts
./start.sh              # Start
./start.sh --migrate    # Run schema.sql then start
./start.sh --build      # Build then run
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?...` | MySQL connection string |
| `UPLOAD_DIR` | `./uploads` | Local storage directory (fallback) |
| `CHUNK_DIR` | `./chunks` | Chunk temp directory |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO endpoint (empty = skip MinIO) |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO access key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO secret key |
| `MINIO_BUCKET` | `filestore` | MinIO bucket name |
| `MINIO_USE_SSL` | `false` | Enable SSL for MinIO |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `REDIS_PASSWORD` | `` | Redis password |

> MinIO 不可用时自动 fallback 到 `UPLOAD_DIR` 本地存储。

## API Endpoints

### File Operations (all require Auth)
| Method | Route | Description |
|--------|-------|-------------|
| POST | `/file/upload` | Upload file (supports instant dedup) |
| GET | `/file/meta` | Get file metadata by hash |
| GET | `/file/query` | List current user's files |
| GET | `/file/download` | Download file by hash |
| POST | `/file/update` | Rename file (op=0) |
| POST | `/file/delete` | Soft delete file |
| POST | `/file/upload/chunk` | Upload a single chunk (idempotent) |
| GET | `/file/upload/status` | Check uploaded chunk indices |
| POST | `/file/upload/merge` | Merge chunks into final file |

### User Operations
| Method | Route | Auth | Rate Limit | Description |
|--------|-------|:----:|:----------:|-------------|
| POST | `/user/signup` | × | ✓ | Register |
| POST | `/user/signin` | × | ✓ | Login, get token (HttpOnly Cookie) |
| GET | `/user/info` | ✓ | × | Get user info |

### System
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/healthz` | Health check |
| GET | `/static/*` | Static frontend pages |

## Architecture Notes

### Auth Flow
1. User POST `/user/signin` with credentials
2. Server verifies bcrypt hash, generates 64-char hex token via `crypto/rand`
3. Token stored in `tbl_user_token` (24h expiry)
4. `Set-Cookie` header sent with `HttpOnly` flag (1h lifetime)
5. `AuthMiddleware` validates Cookie on each `/file/*` request
6. Token is **never** returned in JSON body — only via Cookie

### Storage Layer
- `storage.Storage` interface: `Put(ctx, key, reader, size)` / `Get(ctx, key)` / `Exists(ctx, key)` / `Delete(ctx, key)`
- Two implementations: `MinIOStorage` (S3) and `LocalStorage` (filesystem)
- Selected at startup in `main.go`, injected via `handler.InitStore()`
- MySQL stores metadata only; file content in storage backend

### File Ownership
- `tbl_file.user_name` associates each file with its uploader
- Download/query/rename/delete all verify ownership via `db.GetFileMetaDBByUser()`
- `FileQueryHandler` returns only current user's files
- Unauthorized access returns HTTP 403

### Chunked Upload
1. Client splits file, POSTs each chunk to `/file/upload/chunk` (idempotent)
2. Chunks saved to `<CHUNK_DIR>/<filehash>/<index>`
3. Client GET `/file/upload/status` to check progress
4. POST `/file/upload/merge` triggers server-side concatenation
5. Background cleanup: orphaned chunks removed after 24h (1h check interval)

### Soft Delete
- Sets `tbl_file.status=2`, does not remove file from storage backend
- All queries filter by `status=1` (active)

### Rate Limiting
- IP-based token bucket on `/user/signup` and `/user/signin`
- 5 req/s, burst 10, uses `c.ClientIP()`
- Idle IPs evicted after 5min

## Testing

```bash
go test ./...           # Run all tests
go test -v ./handler/   # Handler tests with verbose output
go test ./util/         # Util tests only
```

Tests cover:
- `util/` — SHA1, MD5, file operations, path utilities
- `handler/` — HTTP responses, status codes, JSON format, auth middleware, rate limiting, user signup/signin validation, edge cases (missing params, invalid input, not-found, panic recovery)

## Development Conventions

- **Language:** Code comments are bilingual (Chinese + English)
- **Naming:** Standard Go conventions — exported PascalCase, unexported camelCase
- **Error handling:** HTTP handlers return JSON via `gin.H{"code": 0|1, "msg": ..., "data": ...}`
- **Logging:** `slog.Info`/`slog.Warn`/`slog.Error` with structured key-value pairs
- **Database:** `db` package wraps MySQL operations; `FileMeta` domain model in `db/file.go`
- **Dependency injection:** `handler.InitStore(store, cfg)` injects storage + config at startup
- **API prefix:** All file routes under `/file` group with `AuthMiddleware`; user routes under `/user`

---

# Project Upgrade Roadmap — 从"练手项目"到"简历亮点"

## 核心目标

修复现有代码中的硬伤 → 引入工业级中间件 → 展示对现代云原生技术栈的理解 → 集成 AI 能力形成差异化

## Phase 0: 修复已知 Bug（面试基础分）

### 0.1 跨用户秒传所有权冲突 🔴
- **问题**：`tbl_file` 主键为 `file_sha1`（全局唯一），用户 B 秒传用户 A 的文件时不会创建 B 的行
- **结果**：B 收到"秒传成功"但 `/file/query` 看不到文件，`/file/download` 404
- **修复**：引入 `user_file` 关联表，秒传时每个用户创建一条指向共享 hash 的记录；或改为 `file_sha1 + user_name` 复合主键 + 全局去重表

### 0.2 并发竞态：TOCTOU + 临时文件冲突 🔴
- **问题 1**：`service/file_service.go:54` 先 `Exists` 再 `Put` 非原子操作
- **问题 2**：`MergeChunks` 临时文件路径为 `<ChunkDir>/<hash>.tmp`，两个用户并发 merge 同一 hash 互相覆盖
- **修复**：用 Redis `SETNX` 或 `sync.Mutex` 互斥锁保护合并操作；引入 Redis 分布式锁

### 0.3 Chunk 无用户隔离 + 路径穿越 🟡
- **问题**：chunk 目录仅按 `fileHash` 组织，任何登录用户可读/覆盖他人分片
- **修复**：chunk 目录加入 `user_name` 层级；`fileHash` 做 40 位 hex 格式校验

### 0.4 软删除无 GC 🟡
- **问题**：`status=2` 后存储层文件永远保留，无回收机制
- **修复**：新增后台 GC 任务，标记删除超过 N 天的文件从存储层移除

### 0.5 Cookie 安全加固 🟡
- **问题**：`SetCookie` 未设 `Secure`、`SameSite`，HTTP 下 token 明文传输
- **修复**：添加 `Secure`（生产环境）、`SameSite=Lax`/`Strict`

---

## Phase 1: Redis 集成（面试高频考点）

### 1.1 引入 go-redis 客户端
- 新增 `redis/` 包，封装连接池
- 配置项：`REDIS_ADDR`、`REDIS_PASSWORD`

### 1.2 秒传去重 Bloom Filter
- 文件上传前先查 Redis Bloom Filter（`BF.EXISTS`）
- 缓存 `file_sha1 → FileMeta` 减少 MySQL 查询
- 设置 TTL 防止缓存膨胀

### 1.3 分布式锁解决并发合并
- MergeChunks 时 `SETNX gofile:lock:merge:<hash>` 防重入
- 自动过期避免死锁

### 1.4 基于 Redis 的限流器
- 替换当前内存令牌桶为 Redis 滑动窗口或令牌桶
- 支持分布式多实例限流

---

## Phase 2: 预签名 URL 直传直下（"眼前一亮"级）

### 2.1 预签名上传
- `GET /file/presigned/upload` → 后端签发 MinIO `presignedPutObject` URL
- 客户端直接 PUT 到 MinIO，不经过应用服务器
- 上传完成后回调通知服务端写入元数据

### 2.2 预签名下载
- `GET /file/presigned/download` → 后端签发 `presignedGetObject` URL
- 客户端直接 GET 文件流，应用服务器零字节拷贝

### 2.3 安全性
- 预签名 URL 绑定：用户、文件 hash、过期时间（5min）
- 应用服务器只签名不传数据，吞吐量不再受应用带宽限制

---

## Phase 3: HTTP Range 断点下载 + 分页

### 3.1 Range 支持
- `DownloadHandler` 解析 `Range` 请求头，返回 `206 Partial Content`
- 支持视频拖动播放、大文件分片下载
- 配套 `Content-Range`、`Accept-Ranges` 响应头

### 3.2 文件列表分页
- `ListByUser` 加 `page`/`size` 参数 + `LIMIT/OFFSET` 或游标分页
- 返回 `total` 计数，前端分页展示

---

## Phase 4: 可观测性（工程化意识）

### 4.1 Prometheus 指标
- `http_requests_total`（按 method/path/status 分桶）
- `http_request_duration_seconds`（直方图）
- `file_upload_bytes_total`（业务指标）
- 用 `gin-prometheus` 或自定义 middleware

### 4.2 /metrics 端点
- 暴露给 Prometheus 抓取

### 4.3 结构化日志增强
- 每个请求附加 `request_id`（UUID 或 Snowflake）
- 跨 handler/service 串联日志

---

## Phase 5: CI/CD + 测试覆盖

### 5.1 GitHub Actions
- `go vet` → `go test -race ./...` → `go build`
- 构建 Docker 镜像
- 可选：自动部署到测试环境

### 5.2 测试补全
- `service/` 层单测（mock repository + storage）
- `repository/` 层测试（使用 `go-sql-driver/mysql` 的 mock 或 testcontainers）
- 目标：全项目 80%+ 覆盖率

### 5.3 压测报告
- wrk/ghz 压测上传/下载/列表接口
- 记录优化前后对比（如"列表接口加索引后 300ms → 8ms"）
- 结果写入 PR 描述或 BENCHMARKS.md

---

## Phase 6: AI 集成（2026 年杀手锏）

### 6.1 文件摘要
- 上传后异步调 LLM API（Claude / OpenAI），读取文本文件内容生成摘要
- 摘要存入 `tbl_file.file_summary` 字段

### 6.2 语义搜索
- 用户输入自然语言查询 → 向量化 → 与文件摘要/文件名做语义匹配
- 用 `/file/ai-search` 端点暴露

### 6.3 智能标签
- 根据文件名和内容自动分类（文档/图片/代码/数据…）
- 前端按标签过滤

---

## Phase 7: 架构增强（远期）

### 7.1 Kafka 异步任务
- 上传 → 发消息 → 异步处理（缩略图、查毒、AI 摘要）
- 解耦"文件上传"和"文件处理"，削峰填谷

### 7.2 K8s 部署
- 编写 `deployment.yaml` + `service.yaml`
- 从 Docker Compose 升级到 minikube 可部署

---

## 升级优先级矩阵

| Phase | 面试价值 | 工作量 | 策略 |
|-------|---------|--------|------|
| P0: 修 Bug | ★★★ | 小 | **立即做** — 这是基础分 |
| P1: Redis | ★★★ | 中 | **立即做** — 面试高频考点 |
| P2: 预签名 URL | ★★★ | 中 | **立即做** — 最亮眼 |
| P3: Range + 分页 | ★★ | 小 | 有空做 |
| P4: 可观测性 | ★★ | 中 | 有空做 |
| P5: CI/CD | ★★ | 中 | 有空做 |
| P6: AI 集成 | ★★★ | 中 | **差异化** — 突破天花板 |
| P7: Kafka/K8s | ★★ | 大 | 有余力再做 |

---

## 简历呈现建议

**不要写：** "基于 Go 的网盘系统，支持上传下载分片"

**要写：**
- 修复了跨用户秒传去重与所有权模型的冲突（全局 hash 去重 × 按用户隔离）
- 用 Redis 分布式锁解决并发分片合并竞态，支持服务器级并发
- 基于预签名 URL 实现零代理文件传输，存储层吞吐量不再受应用服务器带宽限制
- 集成 AI 文件助手：上传后自动生成摘要 + 语义搜索
- 引入 Prometheus 指标 + Grafana 大盘，压测报告附在 PR 描述里（列表接口加索引后 300ms→8ms）