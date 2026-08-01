# CLAUDE.md

## Project Overview

gofile 是一个轻量级网盘服务，使用 Go 编写。支持文件上传/下载、分片上传与断点续传、用户认证、基于 hash 的秒传去重。前端为静态 HTML 页面，后端使用 Gin 框架暴露 JSON API。

## Tech Stack

- **Language:** Go 1.25.0
- **HTTP:** Gin — 路由、中间件、JSON 响应
- **Database:** MySQL via `go-sql-driver/mysql` (连接池, 默认 25 连接)
- **Storage:** 抽象接口 `storage.Storage` — MinIO (S3) 优先, 失败自动 fallback 本地磁盘
- **Auth:** bcrypt + cookie session (token 存 MySQL `tbl_user_token`, 24h 过期)
- **Frontend:** Vanilla HTML + fetch API (no jQuery, no framework)
- **Logging:** `log/slog` (structured JSON logging)
- **Container:** Docker + Docker Compose (app + MySQL + MinIO)

## Project Structure

```
gofile/
├── main.go                入口、路由注册、优雅关闭
├── schema.sql             数据库建表脚本（单文件，包含索引）
├── config/
│   └── config.go          环境变量配置（env 读取 + 默认值）
├── db/
│   ├── mysql/
│   │   └── conn.go        MySQL 连接池（Init/DBConn）
│   ├── file.go            tbl_file CRUD（含软删除、按用户查询）
│   └── user.go            tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go         文件上传/下载/查询/删除 + 分片上传 + 合并
│   ├── handler_test.go    handler 测试（含 auth、限流）
│   ├── user.go            注册/登录/用户信息 + bcrypt + token 生成
│   ├── user_test.go       用户 handler 测试
│   ├── auth.go            AuthMiddleware（Cookie session 验证）
│   ├── ratelimit.go       IP 令牌桶限流中间件（5 req/s, burst 10）
│   └── cleanup.go         定时清理过期 chunk 目录（1h 间隔, 24h 保留）
├── meta/
│   └── filemeta.go        FileMeta 结构体 + MySQL 桥接函数
├── storage/
│   ├── storage.go         Storage 接口定义（Put/Get/Exists/Delete）
│   ├── minio.go           MinIO 对象存储实现
│   └── local.go           本地文件系统实现
├── util/
│   ├── util.go            SHA1、MD5、文件哈希、路径工具
│   ├── util_test.go       工具函数测试
│   └── chunk.go           磁盘-based 分片追踪
├── scripts/
│   ├── start.sh           Unix/macOS 启动脚本（.env 加载 + --migrate/--build）
│   └── start.bat          Windows 启动脚本
├── static/
│   └── view/
│       ├── index.html     首页
│       ├── home.html      用户主页（文件列表、上传）
│       ├── signin.html    登录页
│       └── signup.html    注册页
├── .env.example            环境变量模板
├── Dockerfile              多阶段 Docker 构建
├── docker-compose.yml      Docker Compose 编排（app + MySQL + MinIO）
├── AGENTS.md               AI 开发协作文档
└── README.md              项目说明文档
```

## Build & Run

### Docker (Recommended)
```bash
docker compose up -d
# http://localhost:8080
```

### Manual
```bash
go build -o gofile .
cp .env.example .env       # edit .env
./gofile
```

Or use scripts (loads `.env` automatically):
```bash
./scripts/start.sh              # Start
./scripts/start.sh --migrate    # Run schema.sql then start
./scripts/start.sh --build      # Build binary then run
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
4. Response sets `Set-Cookie` with `HttpOnly` flag (1h lifetime)
5. `AuthMiddleware` validates Cookie on each `/file/*` request
6. Token is **never** returned in JSON body — only via Cookie

### Storage Layer
- `storage.Storage` interface: `Put(ctx, key, reader, size)` / `Get(ctx, key)` / `Exists(ctx, key)` / `Delete(ctx, key)`
- Two implementations: `MinIOStorage` (S3) and `LocalStorage` (filesystem)
- Selected at startup in `main.go`, injected via `handler.InitStore()`
- MySQL stores metadata only; file content in storage backend

### File Ownership
- `tbl_file.user_name` associates each file with its uploader
- Download/query/rename/delete all verify ownership via `GetFileMetaDBByUser()`
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
- **Database:** `db` package wraps MySQL operations; `meta` package bridges DB structs to domain models
- **Dependency injection:** `handler.InitStore(store, cfg)` injects storage + config at startup
- **API prefix:** All file routes under `/file` group with `AuthMiddleware`; user routes under `/user`