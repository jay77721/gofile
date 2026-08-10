<div align="center">

# gofile

**A self-hosted file storage that understands semantics** · Go + Gin + GORM + MySQL + MinIO + Redis + Typesense

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Gin](https://img.shields.io/badge/Gin-1.12-00ADD8?style=flat-square&logo=go)](https://gin-gonic.com)
[![GORM](https://img.shields.io/badge/GORM-1.31-00ADD8?style=flat-square&logo=go)](https://gorm.io)
[![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql&logoColor=white)](https://www.mysql.com)
[![MinIO](https://img.shields.io/badge/MinIO-S3-C72E49?style=flat-square&logo=minio)](https://min.io)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis)](https://redis.io)
[![Typesense](https://img.shields.io/badge/Typesense-Hybrid%20Search-00D4AA?style=flat-square)](https://typesense.org)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](#-license)

> Upload, instant dedup, resumable chunked upload, presigned direct transfer —
> and, most importantly: **find your files in natural language**.
>
> "cache optimization notes" → `Redis in Action.pdf`, `MySQL Tuning.md`, `Go Backend Architecture.docx`

[中文](README_CN.md) · [Features](#-features) · [Quick Start](#-quick-start) · [AI Semantic Search](#-ai-semantic-search) · [API Reference](#-api-reference) · [Observability](#-observability)

</div>

---

## ✨ Features

### 📁 File Operations

| Feature | Description |
|---------|-------------|
| Upload / Instant dedup | SHA1 hashing with Redis-accelerated cache; one physical copy per file globally |
| Chunked upload | Large-file slicing, resumable, idempotent retries, server-side merge (distributed lock) |
| Download | HTTP Range support (`206 Partial Content`), RFC 5987 filename encoding |
| Online preview | Images / PDF / video / audio / text / code; MIME detected by extension + magic bytes |
| Presigned transfer | S3 presigned URLs — client talks to MinIO directly, zero proxy bytes |
| Soft delete + GC | Soft delete leaves storage intact; files older than 7 days with zero active references are reclaimed from storage by a background GC (24h sweep) |

### 🤖 AI Capabilities (optional, `AI_ENABLED=true`)

| Feature | Description |
|---------|-------------|
| Auto summarization | Async LLM call after upload, Chinese summary (≤100 chars) |
| Auto tagging | 3–5 Chinese tags per file, filterable by type |
| Semantic search | Natural-language query → time/type parsing + full-text + vector hybrid (RRF fusion) |
| Similar files | "Find files like this one" via vector KNN |
| Near-duplicates | Duplicate detection with adjustable similarity threshold |

### 🛠️ Engineering

| Feature | Description |
|---------|-------------|
| Auth | bcrypt password hashing + HttpOnly Cookie session (MySQL-backed tokens, 24h expiry) |
| Rate limiting | IP-based limit on signup/signin (5 req/s): Redis-backed Lua fixed window for multi-instance, in-memory token bucket fallback |
| Distributed lock | Redis SETNX + Lua CAS protecting concurrent chunk merges |
| Graceful degradation | MinIO → local disk · Redis → in-memory · Typesense → MySQL LIKE |
| Observability | Prometheus metrics + request_id log correlation + Grafana dashboard |
| Security | 40-hex hash validation (path traversal), per-user chunk isolation, configurable `Secure` cookie |

---

## 🏗️ Architecture

```
                     ┌──────────────────────────────────────────────┐
                     │                Gin HTTP Server               │
                     │  RequestID → Metrics → Recovery middleware   │
                     │                                              │
                     │  ┌──────────┐   ┌──────────────────────┐     │
                     │  │  Handler │──▶│      Service         │     │
                     │  │ auth/rate│   │ file / user / auth / │     │
                     │  │  limit   │   │ ai                   │     │
                     │  └──────────┘   └──────┬───────────────┘     │
                     └────────────────────────┼──────────────────────┘
                                              │
              ┌───────────────────────────────┼───────────────────────────────┐
              │                               │                               │
      ┌───────▼────────┐            ┌─────────▼─────────┐           ┌─────────▼─────────┐
      │  Repository     │            │      Storage      │           │   AI Pipeline     │
      │  GORM + MySQL   │            │  MinIO ⇄ Local    │           │  (async, off-path)│
      │  tbl_file       │            │  (presign/Range)  │           │                   │
      │  tbl_user_file  │            └───────────────────┘           │  Enqueue          │
      │  tbl_user_token │                                            │   └─▶ extract     │
      │  tbl_ai_task    │                                            │   └─▶ analyze(LLM) │
      └─────────────────┘                                            │   └─▶ embed        │
              │                                                      │   └─▶ upsert       │
              │                                                      └────────┬─────────┘
              │                                                               │
      ┌───────▼─────────┐                    ┌───────────────────────────────▼──────────┐
      │     Redis       │                    │          Typesense (Hybrid)              │
      │ dedup/lock/rate │                    │  full-text (filename+summary) + vector   │
      └─────────────────┘                    │  RRF fusion · per-user index isolation   │
                                             └──────────────────────────────────────────┘
```

**Layering**: `handler → service → repository + storage`, one-way dependencies, wired by DI in `main.go`.

### Data Model: Global Dedup × Per-User Ownership

- `tbl_file` — global file registry, unique by `file_sha1`; content stored once
- `tbl_user_file` — ownership rows, `UNIQUE(user_name, file_sha1)`
- Instant dedup = insert an ownership row for the current user when the hash hits — user B "uploading" user A's file still gets full query/download access

---

## 🚀 Quick Start

### Option 1: Docker Compose (recommended)

```bash
git clone git@github.com:jay77721/gofile.git
cd gofile
docker compose up -d
```

| Service | URL | Notes |
|---------|-----|-------|
| 🌐 App | http://localhost:8080 | Main service |
| 🗄️ MinIO console | http://localhost:9001 | `minioadmin` / `minioadmin` |
| 🔍 Typesense | http://localhost:8108 | Search engine |
| 📈 Prometheus | http://localhost:9090 | Metrics scraping |
| 📊 Grafana | http://localhost:3000 | `admin` / `admin`, pre-provisioned gofile Overview dashboard |

> **Enabling AI**: Docker Compose starts with AI off. Add `AI_ENABLED=true` (plus `AI_PROVIDER`, `AI_API_KEY`) to the `app.environment` section of `docker-compose.yml`, then `docker compose up -d` again.

### Option 2: Manual Setup

```bash
# 1. Install dependencies
go mod tidy

# 2. Configure environment
cp .env.example .env    # edit as needed

# 3. Run (GORM AutoMigrate creates tables on startup)
go run main.go

# Or use the scripts
./start.sh              # start with .env
./start.sh --migrate    # run schema.sql first, then start
./start.sh --build      # build binary, then run
```

Windows: use `start.bat` with the same arguments.

---

## 🤖 AI Semantic Search

### Pipeline (async, non-blocking)

```
upload done
   │
   ▼
Enqueue ──▶ worker pool ──▶ extract text (docx/pdf/pptx/txt/zip listing…)
   │                          │
   │                          ▼
   │                    LLM analyze (summary + tags)
   │                          │
   │                          ▼
   │                    Embedding
   │                          │
   │                          ▼
   │                    persist to MySQL + index in Typesense
   ▼
response returned (milliseconds)

failed tasks: auto-requeued while retry_count < 3, purged after 7 days
```

- On dedup hits (global summary already exists) the LLM call is **skipped** — zero-cost indexing
- Task state machine `tbl_ai_task`: `pending → processing → done / failed`, with compensation and TTL cleanup

### Natural-Language Query Parsing

`/file/ai/search?q=` supports combined "time + type + semantic" queries:

| Input | Parsed as |
|-------|-----------|
| `PDFs from the last 3 days` | time: last 3 days · type: `tags:=[文档]` · semantic: "PDFs" |
| `Go interview notes from last week` | time: last week · semantic: "Go interview notes" |
| `images this year` | time: this year · type: `tags:=[图片]` |
| `database optimization` | semantic: "database optimization" (full-text + vector hybrid) |

### Degradation Chain

```
Typesense available ──▶ Hybrid Search (full-text + vector + RRF fusion)
        │
        ▼ unavailable / Embed failed
MySQL LIKE on (filename + summary) — functionality never breaks
```

### Examples

```bash
# Semantic search
curl -b cookies.txt "http://localhost:8080/file/ai/search?q=cache optimization notes"

# Similar files (find files like this one)
curl -b cookies.txt "http://localhost:8080/file/ai/similar?filehash=HASH&limit=5"

# Near-duplicate detection (similarity ≥ 0.9)
curl -b cookies.txt "http://localhost:8080/file/ai/duplicates?filehash=HASH&threshold=0.9"
```

> Zero-cost local try-out: set `AI_PROVIDER=mock` — no API key needed to exercise the whole
> pipeline (deterministic summaries/tags/vectors for development and testing).

---

## ⚙️ Configuration

All settings come from environment variables; a `.env` file is supported (see `.env.example`).

### Core

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?…` | MySQL DSN, AutoMigrate on startup |
| `COOKIE_SECURE` | `false` | Set `true` in production (HTTPS-only cookie) |

### Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `MINIO_ENDPOINT` | `minio:9000` | Empty or unreachable → fallback to local disk |
| `MINIO_ACCESS_KEY` | `minioadmin` | — |
| `MINIO_SECRET_KEY` | `minioadmin` | — |
| `MINIO_BUCKET` | `filestore` | Bucket name |
| `MINIO_USE_SSL` | `false` | — |
| `UPLOAD_DIR` | `./uploads` | Local storage dir (fallback) |
| `CHUNK_DIR` | `./chunks` | Chunk temp dir |

### Cache & Rate Limit (optional)

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_ADDR` | `localhost:6379` | Empty → dedup cache/lock/rate limiter degrade gracefully |
| `REDIS_PASSWORD` | `` | — |
| `REDIS_DB` | `0` | DB number |

### AI (optional, off by default)

| Variable | Default | Description |
|----------|---------|-------------|
| `AI_ENABLED` | `false` | Master switch |
| `AI_PROVIDER` | `mock` | `mock` (free local) / `openai` / `anthropic` |
| `AI_API_KEY` | `` | LLM API key (ignored by mock) |
| `AI_MODEL` | provider default | Model override (e.g. `gpt-4o-mini`) |
| `AI_EMBED_DIM` | `128` | Embedding dimension |
| `AI_WORKERS` | `4` | Async analysis workers |
| `TYPESENSE_URL` | `http://localhost:8108` | Search engine URL |
| `TYPESENSE_API_KEY` | `xyz` | Typesense API key |

---

## 📡 API Reference

Unified response shape: `{"code": 0|error_code, "msg": "...", "data": ...}`, `code=0` means success; HTTP status and business error codes are decoupled.
All `/file/*` routes and `/user/info` require the session cookie set by `/user/signin`.

### Error Codes

| Code | Meaning |
|------|---------|
| `1001` | Missing/invalid params or unsupported operation |
| `1002` | Not logged in or invalid session |
| `1003` | No permission for the resource |
| `1004` | File/resource not found |
| `1005` | Username already exists |
| `1006` | Wrong username or password |
| `1007` | Upload failed |
| `1008` | Chunk merge failed |
| `1009` | Storage error (e.g. presigned URLs require MinIO) |
| `1010` | Too many requests (rate limited) |
| `1011` | AI search failed |
| `1099` | Internal server error (fallback) |

### File Operations (auth required)

| Method | Route | Params | Description |
|--------|-------|--------|-------------|
| `POST` | `/file/upload` | `file` (multipart), optional `filehash` (instant dedup) | Upload; returns `data.filehash` |
| `GET` | `/file/meta` | `filehash` | File metadata (incl. AI summary/tags) |
| `GET` | `/file/query` | optional `page`, `size` (≤100) | File list; with paging returns `{list, total, page, size}` |
| `GET` | `/file/download` | `filehash` | Download; supports `Range` header (206) |
| `GET` | `/file/preview` | `filehash` | Inline preview |
| `POST` | `/file/update` | `op=0`, `filehash`, `filename` | Rename |
| `POST` | `/file/delete` | `filehash` | Soft delete |

### Chunked Upload (auth required)

| Method | Route | Params | Description |
|--------|-------|--------|-------------|
| `POST` | `/file/upload/chunk` | `filehash`, `index`, `file` | Upload one chunk; idempotent, per-user isolation |
| `GET` | `/file/upload/status` | `filehash` | Uploaded chunk indices (resume) |
| `POST` | `/file/upload/merge` | `filehash`, `filename`, `chunks` | Merge chunks (distributed lock + UUID temp file) |

### Presigned Transfer (auth required, MinIO only)

| Method | Route | Params | Description |
|--------|-------|--------|-------------|
| `POST` | `/file/presigned/upload` | `filehash`, `filename` | Issue PUT URL; client uploads straight to MinIO |
| `POST` | `/file/presigned/upload/confirm` | `filehash`, `filename` | Confirm after direct upload, persist metadata |
| `GET` | `/file/presigned/download` | `filehash` | Issue GET URL; client downloads directly |

### AI Search (auth required, AI enabled)

| Method | Route | Params | Description |
|--------|-------|--------|-------------|
| `GET` | `/file/ai/search` | `q`, optional `page`/`size` | Natural-language semantic search |
| `GET` | `/file/ai/similar` | `filehash`, optional `limit` (≤20, default 5) | Similar-file recommendation |
| `GET` | `/file/ai/duplicates` | `filehash`, optional `threshold` (default 0.9) | Near-duplicate detection |

### User & System

| Method | Route | Params | Auth | Rate Limit | Description |
|--------|-------|--------|:----:|:----------:|-------------|
| `POST` | `/user/signup` | `username`, `password` | × | ✓ | Register |
| `POST` | `/user/signin` | `username`, `password` | × | ✓ | Login; token via HttpOnly cookie only |
| `GET` | `/user/info` | — | ✓ | × | User info |
| `GET` | `/healthz` | — | × | × | Health check |
| `GET` | `/metrics` | — | × | × | Prometheus metrics |
| `GET` | `/static/*` | — | × | × | Frontend (Vue 3 SPA) |

### Quick Tour

```bash
# Register & login (cookie saved locally)
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signin -c cookies.txt

# Upload
curl -X POST -F "file=@./test.txt" -b cookies.txt http://localhost:8080/file/upload

# Download with Range
curl -b cookies.txt -H "Range: bytes=0-1023" \
  "http://localhost:8080/file/download?filehash=HASH" -o partial.bin

# Paginated listing
curl -b cookies.txt "http://localhost:8080/file/query?page=1&size=10"

# Presigned upload, three steps
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload          # 1. get URL
curl -X PUT -T ./test.txt "PRESIGNED_URL"               # 2. PUT straight to MinIO
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload/confirm   # 3. confirm

# Semantic search
curl -b cookies.txt "http://localhost:8080/file/ai/search?q=cache optimization notes"
```

---

## 📊 Observability

### Prometheus Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_requests_total` | Counter | `method, path, status` | HTTP request count |
| `http_request_duration_seconds` | Histogram | `method, path` | Request latency |
| `file_upload_bytes_total` | Counter | — | Bytes uploaded |
| `ai_tasks_total` | Counter | `status` | AI task states (pending/done/failed) |
| `ai_llm_duration_seconds` | Histogram | `operation` | LLM call latency |
| `ai_index_ops_total` | Counter | `operation, result` | Index engine operations |

> The `path` label uses the route template (`c.FullPath()`), never raw URLs — keeps label
> cardinality bounded. Docker Compose ships Prometheus + Grafana with a pre-provisioned dashboard.

### request_id Log Correlation

Every request gets a UUID (returned as `X-Request-ID`) injected into its context. A custom
`slog.Handler` appends `request_id` to every log line emitted with `slog.InfoContext/WarnContext/ErrorContext`,
so handler, service and access logs of one request can be correlated:

```json
{"level":"INFO","msg":"access","method":"GET","path":"/file/upload","status":200,"request_id":"fd2d1cdf-..."}
{"level":"INFO","msg":"file uploaded","size":2048,"request_id":"fd2d1cdf-..."}
```

---

## 📁 Project Structure

```
gofile/
├── main.go                 entry: DI wiring, routes, graceful shutdown
├── schema.sql              schema script (reference; AutoMigrate on startup)
├── config/                 env-based config (.env support)
├── model/                  GORM domain models (File/UserFile/User/Token/AITask)
├── repository/             GORM data access (interfaces + impls + mocks)
├── service/                business layer (file/user/auth/AI orchestration)
├── handler/                HTTP layer (files/users/auth/ratelimit/background/AI)
├── ai/                     AI pipeline: Provider abstraction + Typesense index + NLP parsing + text extraction
│   ├── provider.go          Provider interface (analyze/embed/dimension)
│   ├── factory.go           factory: mock | openai | anthropic
│   ├── processor.go         worker pool + task state machine + failure compensation
│   ├── nlp.go               natural-language query parsing (time/type/stopwords)
│   ├── extract.go           text extraction (docx/pdf/pptx/zip…)
│   └── typesense.go         hybrid search index (full-text + vector)
├── storage/                storage abstraction (MinIO ⇄ Local, presign/Range)
├── cache/                  Redis wrapper (dedup cache / distributed lock)
├── metrics/                Prometheus metrics + request_id middleware
├── static/                 frontend Vue 3 SPA (dark mode)
├── deploy/                 Prometheus scrape config + Grafana dashboard provisioning
├── docker-compose.yml      MySQL + MinIO + Redis + Typesense + Prometheus + Grafana
└── start.sh / start.bat    startup scripts
```

---

## 🧰 Tech Stack

| Component | Choice | Purpose |
|-----------|--------|---------|
| Language | Go 1.25 | — |
| Web framework | Gin | Routing & middleware |
| ORM | GORM | AutoMigrate, prepared statements, pool (25 conns) |
| Database | MySQL 8.0 | Metadata (global files / ownership / sessions / AI tasks) |
| Object storage | MinIO (S3) | File content + presigned URLs |
| Search engine | Typesense | Full-text + vector hybrid (RRF) |
| Cache | Redis 7 (optional) | Dedup cache, distributed lock, rate limiting |
| AI | OpenAI / Anthropic / Mock | Summaries, tags, embeddings |
| Metrics | Prometheus client_golang | HTTP + business + AI metrics |
| Monitoring | Grafana | Pre-provisioned dashboard |
| Frontend | Vue 3 (vanilla + CDN) | Dark-mode SPA |
| Deployment | Docker Compose | 7 services, one command |

---

## 🧪 Testing

```bash
go test ./...        # all tests
go vet ./...         # static analysis
```

Covered: `handler/` (HTTP responses, auth, rate limit, edge cases), `metrics/` (request_id,
metrics middleware, full-chain observability), `ai/` (NLP parsing, mock provider, processor
state machine), `util/` (hashing & file utilities).

---

## 📄 License

MIT © [jay77721](https://github.com/jay77721)
