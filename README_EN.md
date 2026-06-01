# FileStore Server

A lightweight file storage server built with Go, supporting file upload, download, user management, and chunked uploads.

[中文](README.md)

## Features

- **File Management** — Upload, download, query, rename, soft delete (100MB limit)
- **User Auth** — Signup/signin with bcrypt, cookie session (SameSite=Strict), 24h token expiry
- **Chunked Upload** — Large file chunking, resumable upload, auto-merge, hash-based instant upload
- **Security** — Path traversal protection, IP rate limiting, input validation, secure random tokens
- **Observability** — Structured JSON logging (log/slog), health check endpoint, graceful shutdown

## Quick Start

### Docker (Recommended)

```bash
git clone <repository-url>
cd filestore-server
docker compose up -d
```

Server starts at `http://localhost:8080` with MySQL and Redis auto-configured.

### Manual

**Prerequisites:** Go 1.25+, MySQL, Redis

```bash
# 1. Install dependencies
go mod tidy

# 2. Set environment variables
export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/fileserver?charset=utf8mb4&parseTime=True&loc=Local"
export REDIS_ADDR="127.0.0.1:6379"

# 3. Initialize database
mysql -u root -p fileserver < migrations/000001_init_schema.up.sql
mysql -u root -p fileserver < migrations/000002_add_indexes.up.sql

# 4. Run
go run main.go
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL connection string |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis address |
| `REDIS_PASS` | (empty) | Redis password |
| `REDIS_DB` | `0` | Redis DB number |
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `UPLOAD_DIR` | `./uploads` | Upload directory |
| `CHUNK_DIR` | `./chunks` | Chunk temp directory |

## API Endpoints

### File Operations (🔒 Auth Required)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/file/upload` | Upload file (supports fast-upload) |
| `GET` | `/file/meta` | Get file metadata |
| `GET` | `/file/query` | List all files |
| `GET` | `/file/download` | Download file |
| `POST` | `/file/update` | Rename file |
| `POST` | `/file/delete` | Soft delete file |

### Chunked Upload (🔒 Auth Required)

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/file/upload/chunk` | Upload single chunk |
| `GET` | `/file/upload/status` | Check uploaded chunks |
| `POST` | `/file/upload/merge` | Merge chunks into file |

### User Operations

| Method | Path | Auth | Rate Limit | Description |
|--------|------|:----:|:----------:|-------------|
| `POST` | `/user/signup` | ✗ | ✓ | Register |
| `POST` | `/user/signin` | ✗ | ✓ | Login, get token |
| `GET` | `/user/info` | ✓ | ✗ | Get user info |

### System

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/healthz` | Health check (Redis ping) |

## Usage Examples

```bash
# Register
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup

# Login
curl -X POST -F "username=test&password=123456" http://localhost:8080/user/signin

# Upload
curl -X POST -F "file=@./test.txt" -b "username=test;token=YOUR_TOKEN" \
  http://localhost:8080/file/upload

# Download
curl -b "username=test;token=YOUR_TOKEN" \
  "http://localhost:8080/file/download?filehash=HASH" -o output.txt
```

## Project Structure

```
filestore-server/
├── main.go              # Entry point, routes, graceful shutdown
├── config/config.go     # Env-based configuration
├── db/
│   ├── mysql/conn.go    # MySQL connection pool
│   ├── file.go          # tbl_file CRUD
│   └── user.go          # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go       # File upload/download/query/delete + chunked upload
│   ├── user.go          # Signup/signin + bcrypt
│   ├── auth.go          # Auth middleware (Cookie session)
│   └── ratelimit.go     # IP rate limiting middleware
├── meta/filemeta.go     # FileMeta struct + MySQL bridge
├── rd/redis.go          # Redis init + hash cache
├── util/
│   ├── util.go          # SHA1, MD5, path utilities
│   ├── chunk.go         # Redis chunk tracking
│   └── resp.go          # JSON response helper
├── migrations/          # SQL migration scripts
├── static/view/         # Frontend HTML pages
├── uploads/             # File storage directory
├── Dockerfile           # Multi-stage build
└── docker-compose.yml   # Docker Compose orchestration
```

## Testing

```bash
go test ./...
go test ./util/...
go test ./handler/...
```

## License

This project is for educational and learning purposes.
