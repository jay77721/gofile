# gofile

<div align="center">

![gofile](https://img.shields.io/badge/gofile-v1.0-blue?style=flat-square&logo=go)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)
![Gin](https://img.shields.io/badge/Gin-1.12-0090D1?style=flat-square&logo=go)
![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql)
![MinIO](https://img.shields.io/badge/MinIO-Latest-C72E49?style=flat-square&logo=minio)
![GORM](https://img.shields.io/badge/GORM-v1.31-00ADD8?style=flat-square&logo=go)
![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)

**Lightweight Self-hosted File Storage**

[English](#features) · [中文](README_CN.md) · [Quick Start](#quick-start) · [API Docs](#api-endpoints) · [Project Structure](#project-structure)

</div>

---

## ✨ Features

| | Feature | Description |
|---|---------|-------------|
| 📤 | **File Upload** | Normal upload + instant dedup via SHA1 hash with Redis cache |
| 📥 | **File Download** | HTTP Range support (206 Partial Content), safe Content-Disposition |
| ⚡ | **Presigned URL** | Direct upload/download to MinIO, zero-proxy file transfer |
| ✂️ | **Chunked Upload** | Large file splitting, resumable, idempotent retry, auto-merge |
| 🔐 | **User Auth** | bcrypt password hashing + HttpOnly Cookie Session |
| 👤 | **File Ownership** | `tbl_user_file` association table, each user owns their copy |
| 🔒 | **Distributed Lock** | Redis SETNX + Lua CAS for concurrent merge safety |
| ⏱️ | **Rate Limiting** | Redis fixed-window counter, multi-instance shared |
| 🛡️ | **Security** | Path traversal protection (40-hex validation), chunk user isolation, RFC 5987 |
| ☁️ | **Storage Backend** | MinIO (S3) prioritized, auto-fallback to local disk |
| 🧹 | **Auto Cleanup** | Expired chunks (1h interval) + orphaned file GC (24h interval) |
| 📊 | **Structured Logs** | JSON log/slog output, easy to collect and analyze |

---

## 🏗️ Architecture

```
┌─────────────┐     ┌─────────────┐     ┌─────────────────┐
│   Browser   │────▶│  Gin HTTP   │────▶│    Handler      │
│  (HTML/JS)  │◀────│   Server    │◀────│  (auth/ratelim) │
└─────────────┘     └─────────────┘     └────────┬────────┘
                                                  │
                    ┌─────────────┐     ┌─────────▼────────┐
                    │    MySQL    │◀────│   Service Layer  │
                    │  (GORM)     │     │  (file/user/auth)│
                    └─────────────┘     └─────────┬────────┘
                                                  │
                              ┌───────────────────┼───────────────────┐
                              │                   │                   │
                    ┌─────────▼────────┐  ┌───────▼───────┐  ┌──────▼──────┐
                    │      MinIO       │  │  Local Disk   │  │    Redis    │
                    │   (S3 compat)    │  │  (fallback)   │  │  (cache)    │
                    └──────────────────┘  └───────────────┘  └─────────────┘
```

---

## 🚀 Quick Start

### Option 1: Docker Compose (Recommended)

```bash
git clone git@github.com:jay77721/gofile.git
cd gofile
docker compose up -d
```

Once started:

| Service | URL | Notes |
|---------|-----|-------|
| 🌐 App | http://localhost:8080 | Main service |
| 🗄️ MinIO | http://localhost:9001 | Credentials: `minioadmin` / `minioadmin` |
| 🔴 Redis | localhost:6379 | Optional, app works without it |

### Option 2: Manual Setup

```bash
# 1. Install dependencies
go mod tidy

# 2. Configure environment
cp .env.example .env
# Edit .env to match your setup

# 3. Initialize database (GORM AutoMigrate handles tables automatically)
go run main.go

# Or manually:
mysql -u root -p gofile < schema.sql
```

Or use startup scripts (loads `.env` automatically):

```bash
./start.sh              # Start with .env
./start.sh --migrate    # Run schema.sql then start
./start.sh --build      # Build binary then run
```

Windows:
```cmd
start.bat              # Start with .env
start.bat --migrate    # Run schema.sql then start
start.bat --build      # Build binary then run
```

---

## ⚙️ Configuration

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
| `REDIS_ADDR` | `localhost:6379` | Redis address (empty = skip Redis) |
| `REDIS_PASSWORD` | `` | Redis password |
| `REDIS_DB` | `0` | Redis database number |
| `COOKIE_SECURE` | `false` | Cookie Secure flag (true in production) |

> 💡 The server attempts MinIO first. If `MINIO_ENDPOINT` is empty or MinIO init fails, it falls back to local storage at `UPLOAD_DIR`. Redis is optional — all Redis features gracefully degrade when unavailable.

---

## 📡 API Endpoints

### File Operations (Auth Required)

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/file/upload` | Upload file (supports instant dedup) |
| `GET` | `/file/meta` | Get file metadata by hash |
| `GET` | `/file/query` | List user files (supports pagination: `?page=1&size=20`) |
| `GET` | `/file/download` | Download file by hash (supports Range header) |
| `GET` | `/file/preview` | Online preview (images/PDF/video/text) |
| `POST` | `/file/update` | Rename file |
| `POST` | `/file/delete` | Soft delete file |

### Presigned URL (Auth Required)

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/file/presigned/upload` | Get presigned PUT URL for direct upload to MinIO |
| `POST` | `/file/presigned/upload/confirm` | Confirm presigned upload completion |
| `GET` | `/file/presigned/download` | Get presigned GET URL for direct download from MinIO |

### Chunked Upload (Auth Required)

| Method | Route | Description |
|--------|-------|-------------|
| `POST` | `/file/upload/chunk` | Upload a single chunk (idempotent, user-isolated) |
| `GET` | `/file/upload/status` | Check uploaded chunk indices |
| `POST` | `/file/upload/merge` | Merge chunks (distributed lock, UUID temp files) |

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
| `GET` | `/metrics` | Prometheus metrics endpoint |
| `GET` | `/static/*` | Static frontend pages |

---

## 💻 Usage Examples

```bash
# Register
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup

# Login (cookie stored locally, auto-sent on subsequent requests)
curl -X POST -F "username=test&password=123456" http://localhost:8080/user/signin -c cookies.txt

# Upload a file
curl -X POST -F "file=@./test.txt" -b cookies.txt \
  http://localhost:8080/file/upload

# Download with Range support
curl -b cookies.txt -H "Range: bytes=0-1023" \
  "http://localhost:8080/file/download?filehash=HASH" -o partial.bin

# List files with pagination
curl -b cookies.txt "http://localhost:8080/file/query?page=1&size=10"

# Presigned upload (two-step)
# Step 1: Get presigned URL (frontend computes SHA1 first)
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload

# Step 2: PUT directly to MinIO
curl -X PUT -T ./test.txt "PRESIGNED_URL"

# Step 3: Confirm
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload/confirm

# Presigned download
curl -b cookies.txt "http://localhost:8080/file/presigned/download?filehash=HASH"
```

---

## 🧪 Testing

```bash
go test ./...           # Run all tests
go test -v ./handler/   # Handler tests with verbose output
go test ./util/         # Util tests only
```

**Coverage:**
- `util/` — SHA1, MD5, file operations, path utilities
- `handler/` — HTTP responses, status codes, JSON format, auth middleware, rate limiting, user signup/signin validation, edge cases
- `metrics/` — request_id middleware/context handler, Prometheus metrics middleware, `/metrics` endpoint
- `handler/observability_test.go` — full-chain observability test (RequestID → Metrics → Recovery)

---

## 📊 Observability

The service exposes Prometheus metrics and correlates logs across layers with a request ID.

### Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | `method`, `path`, `status` | HTTP request count |
| `http_request_duration_seconds` | Histogram | `method`, `path` | Request latency (default buckets) |
| `file_upload_bytes_total` | Counter | — | Bytes of successfully uploaded files |

- **Path label uses the route template** (`c.FullPath()`), not the raw URL — avoids label cardinality explosion from query params / file hashes. Unmatched routes fall back to `unknown`.
- Scrape `GET /metrics` every 15s. `docker compose up -d` starts **Prometheus** (`http://localhost:9090`) and **Grafana** (`http://localhost:3000`, admin/admin) with a pre-provisioned "gofile Overview" dashboard (RPS, P95 latency, 5xx error rate, upload bytes).

### Request ID Correlation

Every request gets a UUID (`X-Request-ID` response header) injected into its request `context`. A custom `slog.Handler` (`metrics.ContextHandler`) extracts it and attaches `request_id` to every log line emitted with `slog.InfoContext/WarnContext/ErrorContext(ctx, ...)` — so handler, service, and access logs for one request can be correlated:

```json
{"level":"INFO","msg":"access","method":"GET","path":"/file/upload","status":200,"request_id":"fd2d1cdf-..."}
{"level":"INFO","msg":"file uploaded","size":2048,"request_id":"fd2d1cdf-..."}
```

Background tasks (chunk cleanup, soft-delete GC) intentionally carry **no** request_id.

---

## 📁 Project Structure

```
gofile/
├── main.go                 # Entry point, DI wiring, route registration, graceful shutdown
├── schema.sql              # Database schema (tables + indexes, reference only)
├── config/
│   └── config.go           # Environment-based configuration (.env support)
├── db/
│   └── mysql/
│       └── conn.go         # GORM connection pool + AutoMigrate
├── model/
│   ├── file.go             # File (global registry) + UserFile (ownership) + FileMeta (DTO)
│   ├── user.go             # User GORM model
│   └── token.go            # Token GORM model
├── repository/
│   ├── file_repo.go        # FileRepository (GORM, with mock)
│   ├── user_repo.go        # UserRepository (GORM, with mock)
│   └── token_repo.go       # TokenRepository (GORM, with mock)
├── service/
│   ├── file_service.go     # Upload/download/merge/presign/dedup business logic
│   ├── user_service.go     # Signup/signin/token generation
│   └── auth_service.go     # Token validation
├── handler/
│   ├── handler.go          # File HTTP handlers (upload/download/merge/presign/range)
│   ├── user.go             # User HTTP handlers (signup/signin/info)
│   ├── auth.go             # Auth middleware (Cookie session)
│   ├── ratelimit.go        # Rate limiting (Redis or in-memory fallback)
│   ├── handler_test.go     # Handler tests
│   ├── observability_test.go # Full-chain observability test
│   └── cleanup.go          # Periodic chunk cleanup + soft-delete GC
├── metrics/
│   ├── metrics.go          # Prometheus metric definitions + /metrics handler
│   ├── middleware.go       # Metrics middleware (counter + histogram + access log)
│   ├── request_id.go       # RequestID middleware + slog context handler
│   ├── metrics_test.go     # Metrics tests
│   └── request_id_test.go  # Request ID tests
├── storage/
│   ├── storage.go          # Storage interface (Put/Get/Exists/Delete/Presign/Range)
│   ├── minio.go            # MinIO S3 implementation
│   └── local.go            # Local filesystem implementation
├── cache/
│   ├── cache.go            # Redis client wrapper
│   ├── hash.go             # File hash dedup cache (Set)
│   └── lock.go             # Distributed lock (SETNX + Lua CAS)
├── util/
│   ├── hash.go             # SHA1, MD5, file hash utilities
│   ├── hash_test.go        # Util tests
│   └── chunk.go            # Disk-based chunk tracking
├── static/                 # Frontend HTML (Vue 3 + Dark Mode SPA)
├── start.sh                # Unix/macOS startup script
├── start.bat               # Windows startup script
├── deploy/
│   ├── prometheus/         # Prometheus scrape config
│   └── grafana/            # Grafana datasource + dashboard provisioning
├── .env.example            # Environment variable template
├── Dockerfile              # Multi-stage Docker build
├── docker-compose.yml      # Docker Compose orchestration (MySQL + MinIO + Redis + Prometheus + Grafana)
├── README_CN.md            # 项目说明文档 (ZH)
└── AGENTS.md               # AI 开发协作文档
```

---

## 🔧 Tech Stack

| Component | Choice | Notes |
|-----------|--------|-------|
| Language | Go 1.25 | Strong typing, concurrency, performance |
| HTTP Framework | Gin | High performance, active ecosystem |
| ORM | GORM | AutoMigrate, model tags, prepared statements |
| Database | MySQL 8.0 | Relational metadata storage |
| Object Storage | MinIO (S3) | Presigned URLs, direct client upload/download |
| Cache | Redis 7 (optional) | File hash dedup cache, distributed lock, rate limiting |
| Auth | bcrypt + Cookie/Session | Password hashing, HttpOnly Cookie |
| Logging | log/slog | Structured JSON output with request_id correlation |
| Metrics | Prometheus client_golang | Custom Gin middleware, `/metrics` endpoint |
| Monitoring | Prometheus + Grafana | Pre-provisioned "gofile Overview" dashboard |
| Deployment | Docker Compose | One-command startup for all services |

---

## 📄 License

MIT

---

<div align="center">

Made with ❤️ by [jay77721](https://github.com/jay77721)

</div>