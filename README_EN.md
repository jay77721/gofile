# FileStore Server

> Gin + MinIO + MySQL + Redis

A lightweight file storage server built with Go, supporting file upload/download, chunked upload with resumable capability, user authentication, and hash-based instant upload deduplication.

[中文](README.md) | [MIT License](LICENSE)

---

## Features

- **File Management** — Upload, download, query, rename, soft delete (100MB limit)
- **Chunked Upload** — Large file splitting, resumable uploads, auto-merge, hash-based instant dedup
- **User Authentication** — Signup/signin with bcrypt, cookie session (SameSite=Strict), 24h token expiry
- **Storage Backend** — MinIO (S3-compatible) with automatic fallback to local filesystem
- **Security** — Path traversal protection, IP token-bucket rate limiting (5 req/s, burst 10), input validation, secure random tokens
- **Observability** — Structured JSON logging (log/slog), health check endpoint (Redis ping), graceful shutdown

## Quick Start

### Docker (Recommended)

```bash
git clone git@github.com:jay77721/FileStore-server.git
cd filestore-server
docker compose up -d
```

Once started:

| Service | URL | Credentials |
|---------|-----|-------------|
| App | http://localhost:8080 | — |
| MinIO Console | http://localhost:9001 | `minioadmin` / `minioadmin` |

MySQL, Redis, and MinIO are all auto-configured by Docker Compose.

### Manual Setup

**Prerequisites:** Go 1.25+, MySQL, Redis, MinIO (optional)

```bash
# 1. Install dependencies
go mod tidy

# 2. Set environment variables
export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/fileserver?charset=utf8mb4&parseTime=True&loc=Local"
export REDIS_ADDR="127.0.0.1:6379"
export MINIO_ENDPOINT="127.0.0.1:9000"
export MINIO_ACCESS_KEY="minioadmin"
export MINIO_SECRET_KEY="minioadmin"
export MINIO_BUCKET="filestore"

# 3. Initialize database
mysql -u root -p fileserver < migrations/000001_init_schema.up.sql
mysql -u root -p fileserver < migrations/000002_add_indexes.up.sql

# 4. Run
go run main.go
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL connection string |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis address |
| `REDIS_PASS` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis DB number |
| `UPLOAD_DIR` | `./uploads` | Upload directory |
| `CHUNK_DIR` | `./chunks` | Chunk temp directory |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO endpoint (empty = skip MinIO) |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO access key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO secret key |
| `MINIO_BUCKET` | `filestore` | MinIO bucket name |
| `MINIO_USE_SSL` | `false` | Enable SSL for MinIO |

> The server attempts MinIO first. If `MINIO_ENDPOINT` is empty or MinIO initialization fails, it falls back to local storage at `UPLOAD_DIR`.

## API Endpoints

### File Operations (Auth Required)

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/file/upload` | Upload file (supports instant dedup) |
| `GET` | `/file/meta` | Get file metadata by hash |
| `GET` | `/file/query` | List all files for current user |
| `GET` | `/file/download` | Download file by hash |
| `POST` | `/file/update` | Rename file |
| `POST` | `/file/delete` | Soft delete file |

### Chunked Upload (Auth Required)

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/file/upload/chunk` | Upload a single chunk |
| `GET` | `/file/upload/status` | Check uploaded chunk indices |
| `POST` | `/file/upload/merge` | Merge chunks into final file |

### User Operations

| Method | Route | Auth | Rate Limit | Description |
|--------|-------|:----:|:----------:|-------------|
| `POST` | `/user/signup` | × | ✓ | Register |
| `POST` | `/user/signin` | × | ✓ | Login, get token |
| `GET` | `/user/info` | ✓ | × | Get user info |

### System

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/healthz` | Health check (Redis ping) |

## Usage Examples

```bash
# Register
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup

# Login (cookie stored locally, auto-sent on subsequent requests)
curl -X POST -F "username=test&password=123456" http://localhost:8080/user/signin

# Upload a file
curl -X POST -F "file=@./test.txt" -b "username=test;token=YOUR_TOKEN" \
  http://localhost:8080/file/upload

# Download a file
curl -b "username=test;token=YOUR_TOKEN" \
  "http://localhost:8080/file/download?filehash=HASH" -o output.txt
```

## Architecture

### Storage Layer

Two storage backends via `storage.Storage` interface:

- **MinIO** — S3-compatible object storage, auto-started in Docker, recommended for production
- **Local** — Local filesystem, used as automatic fallback when MinIO is unavailable

### Auth Flow

1. User POSTs credentials to `/user/signin`
2. Server verifies bcrypt password hash, generates a 64-byte random token
3. Token stored in `tbl_user_token` with 24h expiry
4. `Set-Cookie` header sent with `SameSite=Strict`
5. Subsequent requests validated by `AuthMiddleware` from Cookie

### Chunked Upload Flow

1. Client splits the file into chunks
2. Each chunk POSTed to `/file/upload/chunk`; Redis tracks uploaded indices
3. Client calls `GET /file/upload/status` to check progress
4. After all chunks uploaded, client POSTs `/file/upload/merge`
5. Server concatenates chunks in order, writes to the storage backend
6. Temporary chunk files are cleaned up

## Project Structure

```
filestore-server/
├── main.go                # Entry point, route registration, graceful shutdown
├── config/
│   └── config.go          # Environment-based configuration
├── db/
│   ├── mysql/
│   │   └── conn.go        # MySQL connection pool
│   ├── file.go            # tbl_file CRUD
│   └── user.go            # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go         # File upload/download/query/delete + chunked upload
│   ├── user.go            # Signup/signin + bcrypt
│   ├── auth.go            # Auth middleware (Cookie session)
│   └── ratelimit.go       # IP rate limiting middleware
├── meta/
│   └── filemeta.go        # FileMeta struct + MySQL bridge
├── rd/
│   └── redis.go           # Redis init + file-hash cache
├── storage/
│   ├── storage.go         # Storage interface definition
│   ├── minio.go           # MinIO object storage implementation
│   └── local.go           # Local filesystem storage implementation
├── util/
│   ├── util.go            # SHA1, MD5, file hash, path utilities
│   ├── chunk.go           # Redis chunk tracking helpers
│   └── resp.go            # JSON response helper
├── migrations/            # SQL migration scripts
├── static/view/           # Frontend HTML pages
├── uploads/               # On-disk file storage
├── chunks/                # Temporary chunk storage
├── Dockerfile             # Multi-stage Docker build
└── docker-compose.yml     # Docker Compose orchestration
```

## Testing

```bash
go test ./...           # Run all tests
go test ./util/...      # Run util tests only
go test ./handler/...   # Run handler tests only
```

Coverage includes:
- `util/` — SHA1, MD5, file operations, path utilities, response helpers
- `handler/` — HTTP responses, status codes, JSON format, auth middleware

## Tech Stack

| Component | Choice | Notes |
|-----------|--------|-------|
| HTTP Framework | Gin | High performance, active ecosystem |
| Storage | MySQL + Redis + MinIO | Relational DB + Cache + Object Storage |
| Auth | bcrypt + Cookie/Session | Password hashing, token-based sessions |
| Logging | log/slog | Structured JSON output |
| Deployment | Docker Compose | One-command startup for all services |

## License

MIT
