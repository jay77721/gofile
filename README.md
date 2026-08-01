# gofile

> Gin + MinIO + MySQL

A lightweight file storage server built with Go, supporting file upload/download, chunked upload with resumable capability, user authentication, and hash-based instant upload deduplication.

[中文](README_CN.md) | [MIT License](LICENSE)

---

## Features

- **File Management** — Upload, download, query, rename, soft delete (100MB limit)
- **Chunked Upload** — Large file splitting, resumable uploads, auto-merge, hash-based instant dedup
- **User Authentication** — Signup/signin with bcrypt, cookie session (HttpOnly, SameSite=Strict), 24h token expiry
- **Storage Backend** — MinIO (S3-compatible) with automatic fallback to local filesystem, abstracted via `storage.Storage` interface
- **File Ownership** — Every file is scoped to its uploader; download, query, and rename operations verify ownership
- **Security** — Path traversal protection, IP token-bucket rate limiting (5 req/s, burst 10), trusted proxy configuration, RFC 5987 Content-Disposition encoding, input validation, secure random tokens
- **Observability** — Structured JSON logging (log/slog), health check endpoint, graceful shutdown, periodic chunk cleanup

## Quick Start

### Docker (Recommended)

```bash
git clone git@github.com:jay77721/gofile.git
cd gofile
docker compose up -d
```

Once started:

| Service | URL | Credentials |
|---------|-----|-------------|
| App | http://localhost:8080 | — |
| MinIO Console | http://localhost:9001 | `minioadmin` / `minioadmin` |

MySQL and MinIO are auto-configured by Docker Compose.

### Manual Setup

**Prerequisites:** Go 1.25+, MySQL, MinIO (optional)

```bash
# 1. Install dependencies
go mod tidy

# 2. Configure environment
cp .env.example .env
# Edit .env to match your setup (MYSQL_DSN, MinIO_ENDPOINT, etc.)

# 3. Initialize database
mysql -u root -p gofile < schema.sql

# 4. Run
go run main.go
```

Or use the startup scripts (loads `.env` automatically):

```bash
./scripts/start.sh              # Start with .env
./scripts/start.sh --migrate    # Run migrations then start
./scripts/start.sh --build      # Build binary then run
```

On Windows:

```cmd
scripts\start.bat              # Start with .env
scripts\start.bat --migrate    # Run migrations then start
scripts\start.bat --build      # Build binary then run
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
| `POST` | `/file/upload/chunk` | Upload a single chunk (idempotent) |
| `GET` | `/file/upload/status` | Check uploaded chunk indices |
| `POST` | `/file/upload/merge` | Merge chunks into final file |

### User Operations

| Method | Route | Auth | Rate Limit | Description |
|--------|-------|:----:|:----------:|-------------|
| `POST` | `/user/signup` | × | ✓ | Register |
| `POST` | `/user/signin` | × | ✓ | Login, get token (HttpOnly Cookie) |
| `GET` | `/user/info` | ✓ | × | Get user info |

### System

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/healthz` | Health check |
| `GET` | `/static/*` | Static frontend pages |

## Usage Examples

```bash
# Register
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup

# Login (cookie stored locally, auto-sent on subsequent requests)
curl -X POST -F "username=test&password=123456" http://localhost:8080/user/signin -c cookies.txt

# Upload a file
curl -X POST -F "file=@./test.txt" -b cookies.txt \
  http://localhost:8080/file/upload

# Download a file
curl -b cookies.txt \
  "http://localhost:8080/file/download?filehash=HASH" -o output.txt
```

## Architecture

### Storage Layer

Two storage backends via `storage.Storage` interface (Put/Get/Exists/Delete):

- **MinIO** — S3-compatible object storage, auto-started in Docker, recommended for production
- **Local** — Local filesystem, used as automatic fallback when MinIO is unavailable

The interface is injected at startup via `handler.InitStore()`. MySQL stores metadata; file content lives in the storage backend.

### Auth Flow

1. User POSTs credentials to `/user/signin`
2. Server verifies bcrypt password hash, generates a 64-byte random token
3. Token stored in `tbl_user_token` with 24h expiry
4. `Set-Cookie` header sent with `HttpOnly` flag (1h lifetime)
5. Subsequent requests validated by `AuthMiddleware` from Cookie
6. Token is **never** returned in JSON response body — only via Cookie

### Chunked Upload Flow

1. Client splits the file into chunks
2. Each chunk POSTed to `/file/upload/chunk` (idempotent — retrying the same chunk is safe)
3. Client calls `GET /file/upload/status` to check progress
4. After all chunks uploaded, client POSTs `/file/upload/merge`
5. Server concatenates chunks in order, writes to the storage backend
6. Temporary chunk files and directory are cleaned up
7. Orphaned chunk directories are periodically cleaned up (1h interval, 24h max age)

### File Ownership

Every file is associated with its uploader's username. The following operations are scoped to the authenticated user:

- **Download** — only the owner can download
- **Query** — lists only the current user's files
- **Rename** — only the owner can rename
- **Delete** — soft delete (status=2 in MySQL, file content preserved in storage)

## Project Structure

```
gofile/
├── main.go                # Entry point, route registration, graceful shutdown
├── config/
│   └── config.go          # Environment-based configuration
├── db/
│   ├── mysql/
│   │   └── conn.go        # MySQL connection pool
│   ├── file.go            # tbl_file CRUD (with user-scoped queries)
│   └── user.go            # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go         # File upload/download/query/delete + chunked upload
│   ├── user.go            # Signup/signin + bcrypt
│   ├── auth.go            # Auth middleware (Cookie session)
│   ├── ratelimit.go       # IP rate limiting middleware
│   └── cleanup.go         # Periodic chunk directory cleanup
├── meta/
│   └── filemeta.go        # FileMeta struct + MySQL bridge
├── storage/
│   ├── storage.go         # Storage interface definition
│   ├── minio.go           # MinIO object storage implementation
│   └── local.go           # Local filesystem storage implementation
├── util/
│   ├── util.go            # SHA1, MD5, file hash, path utilities
│   └── chunk.go           # Disk-based chunk tracking helpers
├── scripts/
│   ├── start.sh           # Unix/macOS startup script
│   └── start.bat          # Windows startup script
├── migrations/            # SQL migration scripts
├── static/view/           # Frontend HTML pages
├── uploads/               # On-disk file storage (local fallback)
├── chunks/                # Temporary chunk storage
├── .env.example           # Environment variable template
├── Dockerfile             # Multi-stage Docker build
├── docker-compose.yml     # Docker Compose orchestration
└── AGENTS.md              # Multi-agent development collaboration docs
```

## Testing

```bash
go test ./...           # Run all tests
go test ./util/...      # Run util tests only
go test ./handler/...   # Run handler tests only
```

Coverage includes:
- `util/` — SHA1, MD5, file operations, path utilities
- `handler/` — HTTP responses, status codes, JSON format, auth middleware, rate limiting, user handler tests, edge cases (missing params, invalid input, not-found)

## Tech Stack

| Component | Choice | Notes |
|-----------|--------|-------|
| HTTP Framework | Gin | High performance, active ecosystem |
| Storage | MySQL + MinIO | Relational DB + Object Storage (MinIO with local fallback) |
| Auth | bcrypt + Cookie/Session | Password hashing, token-based sessions |
| Logging | log/slog | Structured JSON output |
| Deployment | Docker Compose | One-command startup for all services |

## License

MIT