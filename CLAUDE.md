# CLAUDE.md

## Project Overview

filestore-server 是一个轻量级网盘服务，使用 Go 编写。支持文件上传/下载、分片上传与断点续传、用户认证、基于 hash 的秒传去重。前端为静态 HTML 页面，后端使用 Gin 框架暴露 JSON API。

## Tech Stack

- **Language:** Go 1.25.0
- **HTTP:** Gin (`github.com/gin-gonic/gin`) — 路由、中间件、JSON 响应
- **Database:** MySQL via `github.com/go-sql-driver/mysql` (连接池, 默认 25 连接)
- **Cache:** Redis via `github.com/redis/go-redis/v9` (文件 hash 缓存 + chunk 索引)
- **Storage:** 抽象接口 `storage.Storage` — MinIO (S3) 优先, 失败自动 fallback 本地磁盘
- **Auth:** bcrypt (`golang.org/x/crypto/bcrypt`) + cookie session (token 存 MySQL `tbl_user_token`)
- **Frontend:** Vanilla HTML + fetch API (no jQuery)
- **Logging:** `log/slog` (structured JSON logging)
- **Container:** Docker + Docker Compose (app + MySQL + Redis + MinIO)

## Project Structure

```
main.go              Entry point, route registration, graceful shutdown
config/
  config.go          Environment-based configuration (env vars with defaults)
db/
  mysql/conn.go      MySQL connection pool (configurable DSN, pool settings)
  file.go            tbl_file CRUD (including soft delete)
  user.go            tbl_user / tbl_user_token CRUD
handler/
  handler.go         File upload/download/query/delete + chunked upload + health check
  user.go            Signup, signin, userinfo + bcrypt + secure token generation
  auth.go            AuthMiddleware (cookie-based auth, JSON responses)
  ratelimit.go       IP-based token bucket rate limiting middleware
meta/
  filemeta.go        FileMeta struct + MySQL bridge functions
rd/
  redis.go           Redis init + file-hash cache
storage/
  storage.go         Storage interface (Put/Get/Exists/Delete)
  minio.go           MinIO object storage implementation
  local.go           Local filesystem implementation
util/
  util.go            SHA1, MD5, file hash, path utilities
  chunk.go           Redis-backed chunk tracking helpers
  resp.go            RespMsg JSON response helper
migrations/          SQL migration scripts
static/view/         Frontend HTML pages (signup, signin, home, upload)
uploads/             On-disk file storage (local fallback)
chunks/              Temporary chunk storage (cleaned up after merge)
Dockerfile           Multi-stage Docker build
docker-compose.yml   Docker Compose with MySQL + Redis + MinIO + App
AGENTS.md            多智能体开发协作文档
```

## Build & Run

### Docker (Recommended)

```bash
docker compose up -d
# Server starts on http://localhost:8080
```

### Manual

```bash
go build -o filestore-server .
export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/fileserver?charset=utf8mb4&parseTime=True&loc=Local"
export REDIS_ADDR="127.0.0.1:6379"
export MINIO_ENDPOINT="127.0.0.1:9000"   # 可选, 留空则使用本地存储
./filestore-server
```

## Configuration

All configuration via environment variables (see `config/config.go`):

| Variable | Default | Description |
|----------|---------|-------------|
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL connection string |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis address |
| `REDIS_PASS` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis DB number |
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `UPLOAD_DIR` | `./uploads` | Local storage directory |
| `CHUNK_DIR` | `./chunks` | Chunk directory |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO endpoint (empty = skip MinIO) |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO Access Key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO Secret Key |
| `MINIO_BUCKET` | `filestore` | MinIO bucket |
| `MINIO_USE_SSL` | `false` | Use SSL for MinIO |

> `MINIO_ENDPOINT` 为空或 MinIO 初始化失败时，自动 fallback 到 `UPLOAD_DIR` 本地存储。

## API Endpoints

### File Operations (🔒 Require Auth via `AuthMiddleware`)

| Method | Route                | Description                       |
|--------|----------------------|-----------------------------------|
| POST   | `/file/upload`       | Upload file (supports fast-upload)|
| GET    | `/file/meta`         | Get file metadata by hash         |
| GET    | `/file/query`        | List all files                    |
| GET    | `/file/download`     | Download file by hash             |
| POST   | `/file/update`       | Rename file (op=0)                |
| POST   | `/file/delete`       | Soft delete file                  |
| POST   | `/file/upload/chunk` | Upload single chunk               |
| GET    | `/file/upload/status`| Check uploaded chunk indices      |
| POST   | `/file/upload/merge` | Merge chunks into final file      |

### User Operations

| Method | Route           | Auth | Rate Limit | Description        |
|--------|-----------------|------|------------|--------------------|
| POST   | `/user/signup`  | No   | Yes        | Register user      |
| POST   | `/user/signin`  | No   | Yes        | Login, get token   |
| GET    | `/user/info`    | Yes  | No         | Get user info      |

### System

| Method | Route     | Description               |
|--------|-----------|---------------------------|
| GET    | `/healthz` | Health check (Redis ping) |
| GET    | `/static/*`| Static frontend pages      |

## Architecture Notes

- **Auth:** Cookie-based session. `AuthMiddleware` wraps all `/file` routes. Tokens are 64-char hex from `crypto/rand`, stored in `tbl_user_token`, expire in 24h. Cookies set with `HttpOnly`, 1h lifetime.
- **Storage abstraction:** `storage.Storage` interface (Put/Get/Exists/Delete). `main.go` selects MinIO or Local at startup, injects via `handler.InitStore()`. MySQL stores metadata; file content lives in the storage backend.
- **Password:** bcrypt with `DefaultCost`. No static salt.
- **Chunked upload:** Chunks saved to `<CHUNK_DIR>/<filehash>/<index>`, index tracked in Redis Set `chunk:<filehash>`. Merge concatenates into storage backend, then cleans up chunks.
- **Fast upload:** Checks `store.Exists()` for existing file hash — returns immediately if found. Redis `SetFileHash` is write-only (cache not read by handlers).
- **Graceful shutdown:** Listens for SIGINT/SIGTERM, drains connections with 10s timeout.
- **Soft delete:** `FileDeleteHandler` sets `status=2` in MySQL, does not remove file from storage.
- **Rate limiting:** IP-based token bucket on `/user/signup` and `/user/signin` (5 req/s, burst 10). Uses `c.ClientIP()`.
- **Logging:** Structured JSON logging via `log/slog`. Levels: Info, Warn, Error.

## Testing

```bash
go test ./...           # Run all tests
go test ./util/...      # Run util tests only
go test ./handler/...   # Run handler tests only
```

Tests cover:
- `util/` — Hash functions (SHA1, MD5), file operations, path utilities, response helpers
- `handler/` — HTTP handler responses, status codes, JSON format, auth interceptor, rate limiting

## Development Conventions

- **Language:** Code comments are bilingual (Chinese + English). README exists in both `README.md` (ZH, primary) and `README_EN.md` (EN).
- **Naming:** Standard Go conventions — exported PascalCase, unexported camelCase, package names lowercase.
- **Error handling:** HTTP handlers return JSON via `gin.H{"code": 0|1, "msg": ..., "data": ...}`. Errors logged via `slog.Error`/`slog.Warn`.
- **Framework:** Gin. Route groups registered in `main.go`. Middleware: `AuthMiddleware`, `RateLimitMiddleware`, `gin.Recovery()`.
- **Logging:** Use `slog.Info`, `slog.Warn`, `slog.Error` with structured key-value pairs.
