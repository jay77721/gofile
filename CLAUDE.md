# CLAUDE.md

> Developer & AI Assistant Orientation Manual — Synchronized with AGENTS.md

## Project Overview

gofile is a lightweight, high-performance self-hosted cloud storage service: Go + Gin + GORM/MySQL + MinIO (S3) + Redis + Typesense + Asynq + Vue 3.
Features include file upload/download, S3 Multipart direct upload & server-side merge, fast-upload deduplication, HTTP Range 206 resumable download, VFS tree directory with Materialized Path, Recycle Bin, file sharing, Asynq distributed task scheduling, and AI semantic search (summarization, auto-tagging, natural language search, similar recommendations, duplicate detection).

## Quick Start

```bash
# 1) Frontend build (output: web/dist; required for /static route)
cd web && npm ci && npm run build && cd ..

# 2) Database: create empty db `gofile` (tables automatically created via migrations/ at startup)
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS gofile CHARACTER SET utf8mb4;"

# 3) Start service
cp .env.example .env
go run main.go        # or: go build -o gofile . && ./gofile
```

## Directory Structure

```
gofile/
├── main.go                Entrypoint, DI assembly, route registration, graceful shutdown, Swagger annotations
├── config/config.go       Environment configuration (Server/MySQL/MinIO/Redis/AI/Cookie/Asynq)
├── model/                 GORM models + DTOs: File/UserFile/User/Token/AITask/AIConfig/Share/Multipart
├── repository/            Data access layer: interfaces + GORM implementations + in-memory mocks
├── service/               Business logic layer: file/user/auth/share/ai/ai_config
├── handler/
│   ├── handler.go         Upload/dedup/download(Range)/preview/trash/chunks/presigned/VFS/S3 Multipart
│   ├── user.go            Signup/signin/logout/user info
│   ├── share.go           Share creation/list/revoke/anonymous download
│   ├── ai.go              Semantic search/similar/duplicate detection
│   ├── ai_config.go       User-level AI Provider configuration
│   ├── auth.go            AuthMiddleware (Cookie session validation)
│   ├── ratelimit.go       IP rate limiting (Redis Lua fixed-window / memory token bucket fallback)
│   ├── cleanup.go         Background workers: chunk cleanup, share expiry, AI retry, task TTL, soft-delete GC
│   └── errcode.go         Unified business error codes + respondError
├── ai/
│   ├── provider.go        Provider interface (Analyze/Embed/Dimension)
│   ├── factory.go         mock | openai | anthropic provider factory
│   ├── openai.go / anthropic.go / mock.go
│   ├── extract.go / pdf.go   Text extraction (text/pdf/office/zip, 1MB budget)
│   ├── nlp.go             Conversational query parser (temporal/type/stopwords)
│   ├── typesense.go / indexer.go / indexer_mock.go   Search index engine
│   └── processor.go       Async task orchestrator (worker pool + state machine; Asynq priority, chan fallback)
├── task/                  M3 Asynq distributed task scheduling (hibiken/asynq)
│   ├── types.go           Task type constants + Payload structs
│   ├── client.go          Producer: Enqueue (implements ai.TaskEnqueuer, idempotent TaskID)
│   ├── server.go          Consumer: NewServer (ai queue weight 6, slog bridged)
│   └── processor.go       Asynq handler → ai.Processor.ProcessOne
├── storage/
│   ├── storage.go         Storage interface (Put/Get/GetRange/FileSize/Exists/Delete/PresignPut/PresignGet/InitMultipart/PresignPartPut/CompleteMultipart/AbortMultipart)
│   ├── minio.go           MinIO implementation (auto bucket creation + presigned URLs + S3 Multipart)
│   └── local.go           Local disk implementation (atomic write; Multipart returns ErrPresignNotSupported)
├── cache/                 Redis wrapper: hash dedup cache + distributed lock
├── metrics/               Prometheus metrics + request_id + access log middleware
├── util/                  SHA1/MD5, chunk tracker, AES-GCM encryption, SSRF URL checker
├── db/mysql/              GORM connection pool + startup migration runner
├── migrations/            golang-migrate versioned SQL migrations (000001_init, 000002_multipart_and_vfs)
├── docs/                  Swagger generated docs (docs.go/swagger.json/yaml)
├── web/                   Vue 3 frontend (Vite + TS + Vitest + ESLint)
├── schema.sql             Full database schema (reference)
├── .github/workflows/ci.yml  CI: gofmt → vet → test -race → build + frontend build + Docker
├── BENCHMARKS.md          Benchmark test report
├── UPGRADE_PLAN.md        Upgrade blueprint and milestone checklist
├── README.md / README_CN.md
└── AGENTS.md              Assistant development guide (synchronized with this file)
```

## API Endpoints

### File Module (`/file`, all authenticated)
| Method | Route | Description |
|--------|-------|-------------|
| POST | `/file/upload` | File upload with fast-upload deduplication (100MB limit) |
| GET | `/file/meta` | File metadata (includes AI summary & tags) |
| GET | `/file/query` | File list (supports `parent_id` folder + `page`/`size` pagination + `breadcrumbs`) |
| GET | `/file/download` | File download (supports HTTP Range 206) |
| GET | `/file/preview` | Online preview with MIME type sniffing |
| POST | `/file/update` | Rename file/folder (op=0) |
| POST | `/file/delete` | Soft delete (move to Recycle Bin) |
| GET | `/file/trash` | Recycle Bin list (paged) |
| POST | `/file/restore` | Restore from Recycle Bin |
| POST | `/file/purge` | Purge permanently from storage & index |
| POST | `/file/share` | Create public share link (days 1-30, optional password) |
| GET | `/file/share/list` | Current user's active share list |
| POST | `/file/share/revoke` | Revoke a share |
| POST | `/file/upload/chunk` | Upload chunk (idempotent) |
| GET | `/file/upload/status` | Query uploaded chunk indexes |
| POST | `/file/upload/merge` | Merge chunks with Redis distributed lock |
| POST | `/file/presigned/upload` | Issue presigned PUT URL (15min) |
| POST | `/file/presigned/upload/confirm` | Confirm presigned upload completed |
| GET | `/file/presigned/download` | Issue presigned GET URL (5min) |
| POST | `/file/upload/multipart/init` | Init S3 Multipart (fast dedup or return part presigned PUT URLs) |
| POST | `/file/upload/multipart/complete` | Complete S3 Multipart (server-side atomic merge) |
| POST | `/file/upload/multipart/abort` | Abort S3 Multipart session |
| POST | `/file/folder/create` | Create folder (`name` + optional `parent_id`) |
| POST | `/file/folder/rename` | Rename file or folder (`file_id` + `new_name`) |
| POST | `/file/folder/move` | Move file or folder (`file_id` + `target_parent_id`; circular move prevention) |
| GET | `/file/ai/search` | Semantic hybrid search (registered when AI_ENABLED=true) |
| GET | `/file/ai/similar` | Similar file recommendations |
| GET | `/file/ai/duplicates` | Near-duplicate detection |

### User Module (`/user`)
| Method | Route | Auth | Rate Limit | Description |
|--------|-------|:----:|:----------:|-------------|
| POST | `/user/signup` | × | 5/s burst 10 | User registration |
| POST | `/user/signin` | × | 5/s burst 10 | User login, sets HttpOnly Cookie |
| POST | `/user/logout` | × | × | Logout (invalidates token & clears Cookie) |
| GET | `/user/info` | ✓ | × | Current user info |

### Share Module (Public)
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/share/:token?pwd=` | Anonymous share download (rate limited 10/s burst 20, Range support) |

### AI Configuration (`/ai/config`, authenticated)
| Method | Route | Description |
|--------|-------|-------------|
| GET / POST / DELETE | `/ai/config` | Read (masked key) / Save / Clear user-level Provider config |
| POST | `/ai/config/test` | Connectivity test (chat + embedding double probe) |

### System & Metrics
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/healthz` | Health check |
| GET | `/metrics` | Prometheus metrics endpoint |
| GET | `/static/*` | Frontend static assets (`web/dist`) |
| GET | `/` | Redirect to `/static/index.html` |

## Core Architecture Principles

### 1. Layered Architecture & Dependency Injection
- Strict unidirectional dependency: `handler → service → repository + storage/cache/ai/task`.
- All dependencies assembled via constructor injection (`NewXxxService(repo, store, ...)`). No package-level global variables or implicit shared states.
- Optional dependencies (Redis/AI/Typesense/Asynq) must be injected in a nil-safe manner with automatic fallback.

### 2. Response Format & Unified Error Codes
- Uniform JSON response: `gin.H{"code": 0|errorCode, "msg": "...", "data": ...}`, success `code = 0`.
- All handlers use `respondError(c, httpStatus, code, msg)` (`handler/errcode.go`).
- Standard codes: 1001 InvalidParams / 1002 Unauthorized / 1003 Forbidden / 1004 NotFound / 1005 UserExists / 1006 InvalidCreds / 1007 UploadFailed / 1008 MergeFailed / 1009 StorageError / 1010 TooManyRequests / 1011 SearchFailed / 1099 InternalError.

### 3. File Ownership Model (Core Invariant)
- `tbl_file` dedup globally by SHA1; `tbl_user_file` maps user ownership and VFS paths.
- Fast Upload: on global hash hit, insert a `tbl_user_file` relation row for current user and enqueue AI task.
- **Every file operation** must verify ownership via `fileRepo.GetByHash(filehash, username)`. Return 1003 Forbidden if unauthorized.

### 4. VFS (Virtual File System)
- Materialized Path: `dir_path` (e.g., `/Go/Sources/`) enables efficient subtree queries using prefix matching.
- Circular Move Prevention: moving a folder into its own subfolder is strictly forbidden.
- Atomic Prefix Updates: folder rename/move updates child paths atomically using SQL `CONCAT` + `SUBSTRING`.

### 5. S3 Multipart & Zero-Disk I/O
- Direct Multipart Upload: client PUTs slices directly to MinIO; backend performs atomic merge on MinIO server side without local disk I/O or relay bandwidth overhead.
- Storage abstraction: MinIO first, local disk fallback (atomic write: temp file + rename); delete storage on DB insert failure (`store.Delete(ctx, key)`).

### 6. Asynq Distributed Task Scheduling (M3)
- Dual-path Task Queue: `ASYNQ_ENABLED=true` uses `task.Client` (Redis persistence, worker pool, MaxRetry=3 backoff, dead-letter queue); seamlessly falls back to in-process memory chan if Redis is unavailable.
- Idempotency: TaskID is `username:filehash`; backed by MySQL `tbl_ai_task` status machine.

---

## ⚡ Development & Contribution Conventions (CRITICAL RULES)

### 1. Git Commit Convention (MUST BE IN ENGLISH)
**All Git commit messages MUST be written strictly in English using the Conventional Commits format**:
- Format: `<type>(<scope>): <subject in english>`
- Types:
  - `feat`: New features (e.g., `feat(task): implement asynq distributed worker pool`)
  - `fix`: Bug fixes (e.g., `fix(service): trigger ai enqueue on fast upload`)
  - `docs`: Documentation updates (e.g., `docs: sync AGENTS.md and CLAUDE.md specifications`)
  - `refactor`: Code refactoring (e.g., `refactor(ai): extract ProcessOne for task handler reuse`)
  - `test`: Adding or updating tests (e.g., `test(repository): add sqlite mock tests for vfs queries`)
  - `chore`/`build`/`ci`: Build configs, tooling, and dependency updates

### 2. Code Conciseness & Zero-Redundancy (Keep It Simple)
- **Clean & Concise**: Follow Go idioms; keep code flat, direct, and readable. Avoid unnecessary layers, over-engineering, or meaningless wrapper structs.
- **No Redundancy (DRY)**: Centralize common logic into single sources of truth. Never copy-paste logic across handlers, services, or repositories.
- **Single Responsibility**: Keep functions, methods, and interfaces focused and minimal.
- **Robust Error Handling**: Check errors on all I/O, DB, and network calls; log structured context with `slog.*Context` (automatically includes `request_id`).
- **Test Integrity**: Unit tests use in-memory SQLite / Mocks; `go test -race ./...` must always pass.

---

## Configuration Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?...` | MySQL DSN (required, auto-migrated on start) |
| `MINIO_ENDPOINT` | `127.0.0.1:9000` | MinIO endpoint (empty falls back to local storage) |
| `REDIS_ADDR` | `localhost:6379` | Redis address (optional: dedup cache, locks, rate limit, Asynq) |
| `ASYNQ_ENABLED` | `false` | Enable Asynq persistent task queue (requires Redis; recommended in production) |
| `AI_ENABLED` | `false` | Master switch for AI capabilities |
| `AI_PROVIDER` | `mock` | `mock` \| `openai` \| `anthropic` |
| `TYPESENSE_URL` | `http://localhost:8108` | Typesense search engine URL (falls back to MySQL LIKE) |
| `COOKIE_SECURE` | `false` | HTTPS-only Cookie transmission (set true in production) |
| `ALLOW_PRIVATE_AI_URL` | `false` | Allow custom AI baseURL pointing to private IPs (e.g., local Ollama) |

## Known Issues & Status

- ~~🔴 `handler/handler.go` FastUpload missing enqueue~~ ✅ **Fixed** (`50d7309`): `FastUpload()` triggers AI task enqueue
- 🟡 `web/dist` not in git: run `cd web && npm run build` before launching server, otherwise `/static` returns 404
- 🟡 Range open intervals (`bytes=N-`) return 416 if file size is unknown
- 🟡 Pagination uses `LIMIT/OFFSET`; cursor pagination recommended for massive datasets

## Roadmap Status

**Completed**:
- P0: 5 core bug fixes + GORM migration
- P1: Redis (dedup cache, distributed locks, rate limiting)
- P2: Presigned URLs for direct upload/download
- P3: HTTP Range 206 + pagination
- P4: Observability (Prometheus + Grafana + request_id tracing)
- P6: AI Semantic Search (Typesense hybrid search, NLP parsing, auto-tagging, recommendations)
- **M1**: S3 Multipart direct upload & server-side merge (`c9d150f`)
- **M2**: VFS Virtual File System (Materialized Path, tree navigation, breadcrumbs) (`c9d150f`)
- **M3**: Asynq distributed task scheduling (`50d7309`)
- Engineering: GitHub Actions CI, Swagger, golang-migrate, BENCHMARKS, SQLite test suite

**Pending / Next Up**:
- P5.2: Test coverage push to ≥80% (VFS/Multipart/task test suites)
- M4: Drive RAG Q&A (document chunking + SSE stream chat)
- M5: WebDAV protocol support (`golang.org/x/net/webdav`)
- P7: Kubernetes deployment (Helm + HPA + ConfigMap)
