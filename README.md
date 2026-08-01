# gofile

<div align="center">

![gofile](https://img.shields.io/badge/gofile-v1.0-blue?style=flat-square&logo=go)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)
![Gin](https://img.shields.io/badge/Gin-1.12-0090D1?style=flat-square&logo=go)
![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql)
![MinIO](https://img.shields.io/badge/MinIO-Latest-C72E49?style=flat-square&logo=minio)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)

**Lightweight Self-hosted File Storage**

[English](#features) · [中文](README_CN.md) · [Quick Start](#quick-start) · [API Docs](#api-endpoints) · [Project Structure](#project-structure)

</div>

---

## ✨ Features

| | Feature | Description |
|---|---------|-------------|
| 📤 | **File Upload** | Normal upload + instant dedup via SHA1 hash |
| 📥 | **File Download** | Range request support, safe Content-Disposition encoding |
| ✂️ | **Chunked Upload** | Large file splitting, resumable, idempotent retry, auto-merge |
| 🔐 | **User Auth** | bcrypt password hashing + HttpOnly Cookie Session |
| 👤 | **File Ownership** | Every file scoped to uploader; all operations verify ownership |
| 🛡️ | **Security** | Path traversal protection, IP rate limiting (5 req/s, burst 10), RFC 5987 encoding |
| ☁️ | **Storage Backend** | MinIO (S3) prioritized, auto-fallback to local disk |
| 🧹 | **Auto Cleanup** | Expired chunks cleaned periodically (1h interval, 24h retention) |
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
                    │    MySQL    │◀────│   db/meta Layer  │
                    │  (metadata) │     │   (FileMeta)     │
                    └─────────────┘     └─────────┬────────┘
                                                  │
                              ┌───────────────────┼───────────────────┐
                              │                   │                   │
                    ┌─────────▼────────┐  ┌───────▼───────┐   ┌───────▼───────┐
                    │      MinIO       │  │  Local Disk   │   │   (future)    │
                    │   (S3 compat)    │  │  (fallback)   │   │               │
                    └──────────────────┘  └───────────────┘   └───────────────┘
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

### Option 2: Manual Setup

```bash
# 1. Install dependencies
go mod tidy

# 2. Configure environment
cp .env.example .env
# Edit .env to match your setup

# 3. Initialize database
mysql -u root -p gofile < schema.sql

# 4. Run
go run main.go
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

> 💡 The server attempts MinIO first. If `MINIO_ENDPOINT` is empty or MinIO init fails, it falls back to local storage at `UPLOAD_DIR`.

---

## 📡 API Endpoints

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

# Download a file
curl -b cookies.txt \
  "http://localhost:8080/file/download?filehash=HASH" -o output.txt
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
- `handler/` — HTTP responses, status codes, JSON format, auth middleware, rate limiting, user signup/signin validation, edge cases (missing params, invalid input, not-found, panic recovery)

---

## 📁 Project Structure

```
gofile/
├── main.go                 # Entry point, route registration, graceful shutdown
├── schema.sql              # Database schema (tables + indexes, single file)
├── config/
│   └── config.go           # Environment-based configuration
├── db/
│   ├── mysql/
│   │   └── conn.go         # MySQL connection pool
│   ├── file.go             # tbl_file CRUD + FileMeta domain model
│   └── user.go             # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go          # File upload/download/query/delete + chunked upload
│   ├── user.go             # Signup/signin + bcrypt + token generation
│   ├── auth.go             # Auth middleware (Cookie session)
│   ├── ratelimit.go        # IP rate limiting middleware
│   └── cleanup.go          # Periodic chunk directory cleanup
├── storage/
│   ├── storage.go          # Storage interface definition
│   ├── minio.go            # MinIO object storage implementation
│   └── local.go            # Local filesystem storage implementation
├── util/
│   ├── hash.go             # SHA1, MD5, file hash, path utilities
│   └── chunk.go            # Disk-based chunk tracking helpers
├── static/                 # Frontend HTML pages
├── start.sh                # Unix/macOS startup script
├── start.bat               # Windows startup script
├── .env.example            # Environment variable template
├── Dockerfile              # Multi-stage Docker build
├── docker-compose.yml      # Docker Compose orchestration
├── README_CN.md            # 项目说明文档 (ZH)
└── AGENTS.md               # AI 开发协作文档
```

---

## 🔧 Tech Stack

| Component | Choice | Notes |
|-----------|--------|-------|
| HTTP Framework | Gin | High performance, active ecosystem |
| Storage | MySQL + MinIO | Relational DB + Object Storage (MinIO with local fallback) |
| Auth | bcrypt + Cookie/Session | Password hashing, token-based sessions |
| Logging | log/slog | Structured JSON output |
| Deployment | Docker Compose | One-command startup for all services |

---

## 📄 License

MIT

---

<div align="center">

Made with ❤️ by [jay77721](https://github.com/jay77721)

</div>
