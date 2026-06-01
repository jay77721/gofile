# CLAUDE.md

## Project Overview

filestore-server 是一个轻量级网盘服务，使用 Go 编写。支持文件上传/下载、分片上传与断点续传、用户认证、基于 hash 的秒传去重。前端为静态 HTML 页面，后端使用标准库 `net/http` 暴露 JSON API。

## Tech Stack

- **Language:** Go 1.25.0
- **HTTP:** Standard library `net/http` (no router framework)
- **Database:** MySQL via `github.com/go-sql-driver/mysql`
- **Cache:** Redis via `github.com/redis/go-redis/v9`
- **Auth:** bcrypt (`golang.org/x/crypto/bcrypt`) + cookie session
- **Frontend:** Vanilla HTML + fetch API (no jQuery)
- **Logging:** `log/slog` (structured JSON logging)
- **Container:** Docker + Docker Compose

## Project Structure

```
main.go              Entry point, route registration, graceful shutdown
config/
  config.go          Environment-based configuration (env vars with defaults)
db/
  mysql/conn.go      MySQL connection pool (configurable DSN)
  file.go            tbl_file CRUD (including soft delete)
  user.go            tbl_user / tbl_user_token CRUD
handler/
  handler.go         File upload/download/query/delete + chunked upload + health check
  user.go            Signup, signin, userinfo + bcrypt + secure token generation
  auth.go            HTTPInterceptor middleware (cookie-based auth, JSON responses)
  ratelimit.go       IP-based rate limiting middleware
meta/
  filemeta.go        FileMeta struct + MySQL bridge functions (no in-memory map)
rd/
  redis.go           Redis init + file-hash cache (configurable via env vars)
util/
  util.go            SHA1, MD5, file hash, path utilities
  chunk.go           Redis-backed chunk tracking helpers
  resp.go            RespMsg JSON response helper
migrations/          SQL migration scripts
static/view/         Frontend HTML pages (signup, signin, home, upload)
uploads/             On-disk file storage
chunks/              Temporary chunk storage (cleaned up after merge)
Dockerfile           Multi-stage Docker build
docker-compose.yml   Docker Compose with MySQL + Redis + App
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
| `UPLOAD_DIR` | `./uploads` | Upload directory |
| `CHUNK_DIR` | `./chunks` | Chunk directory |

## API Endpoints

### File Operations (🔒 Require Auth)

| Method | Route                  | Description                       |
|--------|------------------------|-----------------------------------|
| GET    | `/file/upload`         | Serve upload page                 |
| POST   | `/file/upload`         | Upload file (supports fast-upload)|
| GET    | `/file/upload/suc`     | Upload success page               |
| GET    | `/file/meta`           | Get file metadata by hash         |
| GET    | `/file/query`          | List all files                    |
| GET    | `/file/download`       | Download file by hash             |
| POST   | `/file/update`         | Rename file (op=0)                |
| POST   | `/file/delete`         | Soft delete file                  |
| POST   | `/file/upload/chunk`   | Upload single chunk               |
| GET    | `/file/upload/status`  | Check uploaded chunk indices      |
| POST   | `/file/upload/merge`   | Merge chunks into final file      |

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

## Architecture Notes

- **Auth:** Cookie-based session. `HTTPInterceptor` wraps all file routes. Tokens are 64-char hex from `crypto/rand`, expire in 24h. Cookies have `SameSite=Strict`.
- **Storage:** MySQL is the single source of truth. Redis used for caching (file hash → location) and chunk tracking (Redis Sets).
- **Password:** bcrypt with `DefaultCost`. No static salt.
- **Chunked upload:** Chunks saved to `./chunks/<filehash>/<index>`, tracked in Redis Sets. Merge concatenates into `./uploads/`, cleans up chunks.
- **Fast upload:** Checks Redis cache then MySQL for existing file hash — returns immediately if found.
- **Graceful shutdown:** Listens for SIGINT/SIGTERM, drains connections with 10s timeout.
- **Soft delete:** `FileDeleteHandler` sets `status=2` in MySQL, does not remove file from disk.
- **Rate limiting:** IP-based token bucket on `/user/signup` and `/user/signin` (5 req/s, burst 10).
- **Logging:** Structured JSON logging via `log/slog`. Levels: Info, Warn, Error.

## Testing

```bash
go test ./...           # Run all tests
go test ./util/...      # Run util tests only
go test ./handler/...   # Run handler tests only
```

Tests cover:
- `util/` — Hash functions (SHA1, MD5), file operations, path utilities, response helpers
- `handler/` — HTTP handler responses, status codes, JSON format, auth interceptor

## Development Conventions

- **Language:** Code comments are bilingual (Chinese + English). README exists in both `README.md` (EN) and `README_ZH.md` (ZH).
- **Naming:** Standard Go conventions — exported PascalCase, unexported camelCase, package names lowercase.
- **Error handling:** HTTP handlers return JSON via `util.RespMsg` and `writeJSON()` helper. Errors logged via `slog.Error`/`slog.Warn`.
- **No framework:** Pure `net/http`. Route params parsed from query strings or form values.
- **Logging:** Use `slog.Info`, `slog.Warn`, `slog.Error` with structured key-value pairs.
