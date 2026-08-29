<div align="center">

# gofile

**Production-grade, AI-powered lightweight self-hosted cloud drive** · Go 1.25 + Gin + GORM + MySQL + MinIO (S3) + Redis + Asynq + Typesense + Vue 3 (TS)

[![CI](https://github.com/jay77721/gofile/actions/workflows/ci.yml/badge.svg)](https://github.com/jay77721/gofile/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Gin](https://img.shields.io/badge/Gin-1.12-00ADD8?style=flat-square&logo=go)](https://gin-gonic.com)
[![GORM](https://img.shields.io/badge/GORM-1.31-00ADD8?style=flat-square&logo=go)](https://gorm.io)
[![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql&logoColor=white)](https://www.mysql.com)
[![MinIO](https://img.shields.io/badge/MinIO-S3%20Multipart-C72E49?style=flat-square&logo=minio)](https://min.io)
[![Asynq](https://img.shields.io/badge/Asynq-Distributed%20Queue-FF6B6B?style=flat-square)](https://github.com/hibiken/asynq)
[![Typesense](https://img.shields.io/badge/Typesense-Hybrid%20Search-00D4AA?style=flat-square)](https://typesense.org)
[![Vue 3](https://img.shields.io/badge/Vue-3.5%20%2B%20TS-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue?style=flat-square)](LICENSE)

> Instant fast-upload dedup, S3 multipart direct upload with atomic server-side merge, VFS tree directory, HTTP Range 206 resumable download —
> and, most importantly: **manage and retrieve your knowledge assets using natural language**.
>
> "Find distributed lock and database optimization materials from last week" → Millisecond-level hybrid search results with AI summaries and auto-generated tags.

[中文文档](README_CN.md) · [Features](#-features) · [Quick Start](#-quick-start) · [Architecture](#-architecture) · [AI Semantic Search](#-ai-semantic-search) · [API Reference](#-api-reference) · [Observability](#-observability)

</div>

---

## ✨ Features

### 📁 Storage & File Capabilities

| Feature | Technical Implementation & Description |
|---|---|
| **Instant Dedup (Fast Upload)** | SHA1 hash deduplication + Redis cache acceleration; globally unique physical storage with millisecond-level multi-user ownership association |
| **S3 Multipart Direct Upload (M1)** | Batch presigned PUT URLs for direct client-to-MinIO streaming, server-side atomic merge (zero proxy I/O), and automatic 24h background GC for abandoned uploads |
| **Virtual File System - VFS (M2)** | Hierarchical directory tree, Materialized Paths (`dir_path`), create/rename/move, deep anti-circular nested move prevention, and breadcrumb navigation |
| **Streaming & Resumable Download** | HTTP Range 206 partial content request handling for video seeking and large-file resume |
| **Intelligent Online Preview** | Double-layer safe MIME detection (extension + magic bytes), supporting inline preview for images, PDF, code, text, video, and audio |
| **Recycle Bin & Safe Cascading Purge** | Complete isolation between soft-deleted and active files; purge operations inspect global reference counts and cascade deletion to physical storage and AI indexes only when references reach zero |

### 🤖 Intelligent AI Semantic Pipeline (`AI_ENABLED=true`)

| Feature | Technical Implementation & Description |
|---|---|
| **Distributed Task Orchestration (M3)** | Industrial-grade task queue powered by `hibiken/asynq` + Redis, featuring worker pools, exponential backoff retries (MaxRetry=3), dead-letter queues, and graceful in-memory channel fallback |
| **Auto Summarization & Tagging** | Asynchronously extracts content and prompts LLM to generate concise summaries (≤100 chars) and structured tags (Docs/Code/Media, etc.) |
| **Natural Language Hybrid Search** | Natural-language query parsing (temporal resolution like "past 3 days" + type filtering + semantic intent) + full-text & vector hybrid search (RRF fusion) |
| **Per-User AI Provider Config** | Web UI settings allowing users to configure custom OpenAI-compatible endpoints with AES-GCM encrypted key storage and masked response |
| **Similar Recommendations & Dedup** | Vector KNN cosine similarity matching for "find similar documents" and near-duplicate detection |

### 🛠️ Production Engineering & Security

* **Layered Modular Architecture**: `internal/transport/http` handles HTTP, `internal/application/service` owns use cases, and `internal/port` separates those use cases from `internal/infrastructure` adapters.
* **Security Guardrails**: 40-character Hex hash validation (path traversal defense), dangerous extension blacklisting (stored XSS defense), HttpOnly + Secure cookies.
* **Distributed Rate Limiting & Locking**: Redis + Lua fixed-window rate limiting for auth endpoints (5 req/s) and distributed locks protecting concurrent chunk merges.
* **End-to-End Observability**: Request-scoped `X-Request-ID` log correlation + Prometheus metrics collection + pre-configured Grafana dashboard.
* **Full-Stack TypeScript**: 100% Vue 3 SFC `<script setup lang="ts">` with unified API contracts and type safety.

---

## 🏗️ Architecture & Topology

```
                             ┌──────────────────────────────────────────────────┐
                             │                 Gin HTTP Server                  │
                             │   RequestID ──▶ Prometheus ──▶ RateLimit Chain   │
                             └────────────────────────┬─────────────────────────┘
                                                      │
                       ┌──────────────────────────────┴──────────────────────────────┐
                       ▼                                                             ▼
           ┌────────────────────────┐                                   ┌────────────────────────┐
           │ internal/transport/http│                                   │ internal/application/ │
           │      /handler          │                                   │       /service        │
           │ • vfs.go (Folders)     │──────────────────────────────────▶│ • vfs_service.go (VFS) │
           │ • multipart.go (S3)    │                                   │ • multipart_service.go │
           │ • download.go (Range)  │                                   │ • trash_service.go     │
           └────────────────────────┘                                   └───────────┬────────────┘
                                                                                     │
                       ┌─────────────────────────────────────────────────────────────┼─────────────────────────────┐
                       ▼                                                             ▼                             ▼
           ┌────────────────────────┐                                   ┌────────────────────────┐    ┌────────────────────────┐
           │ internal/infrastructure│                                   │ internal/infrastructure│    │ internal/infrastructure│
           │ GORM + MySQL 8.0       │                                   │ MinIO (S3) ⇄ Local     │    │ Asynq Redis Queue      │
           │ • tbl_file (Dedup)     │                                   │ • S3 Multipart Direct  │    │ (Worker Pool / Retry)  │
           │ • tbl_user_file (VFS)  │                                   │ • HTTP Range 206       │    │ (Fallback In-Mem Chan) │
           │ • tbl_multipart_upload │                                   └────────────────────────┘    └───────────┬────────────┘
           └────────────────────────┘                                                                              │
                       │                                                                                           ▼
                       ▼                                                                              ┌────────────────────────┐
           ┌────────────────────────┐                                                                 │ internal/infrastructure│
           │     /cache/redis       │                                                                 │     /ai                │
           │ Fast-upload/Lock/Limit │                                                                 │ • LLM Summary & Tags   │
           └────────────────────────┘                                                                 │ • Typesense Vector KNN │
                                                                                                      └────────────────────────┘
```

`internal/app` is the composition root: it loads configuration, opens infrastructure resources, wires `internal/port` contracts, and owns shutdown. Concrete MySQL, Redis, storage, queue, and AI adapters remain below `internal/infrastructure`; `internal/observability/metrics` provides request and business metrics.

---

## 🚀 Quick Start

### Option 1: Docker Compose (Recommended)

```bash
git clone https://github.com/jay77721/gofile.git
cd gofile
docker compose -f docker/docker-compose.yml up -d
```

Service Endpoints:
* 🌐 **Web App**: `http://localhost:8080`
* 🗄️ **MinIO Console**: `http://localhost:9001` (Credentials: `minioadmin` / `minioadmin`)
* 🔍 **Typesense Search Engine**: `http://localhost:8108`
* 📈 **Prometheus Metrics**: `http://localhost:9090`
* 📊 **Grafana Dashboard**: `http://localhost:3000` (Credentials: `admin` / `admin`, includes pre-configured Gofile dashboard)

---

### Option 2: Local Development

```bash
# 1. Build frontend (Vue 3 + Vite + TypeScript)
cd web
npm install
npm run build
cd ..

# 2. Install backend dependencies and configure environment
go mod tidy
cp .env.example .env  # edit .env: MySQL is required; Redis/MinIO/Typesense/AI are optional (graceful fallback)
# All keys are documented in .env.example (mirrors internal/config/config.go):
# SERVER_ADDR, COOKIE_SECURE, MYSQL_DSN, UPLOAD_DIR/CHUNK_DIR,
# MINIO_*, REDIS_ADDR/PASSWORD/DB, AI_ENABLED/PROVIDER/API_KEY/MODEL/EMBED_DIM/WORKERS,
# TYPESENSE_*, AI_CONFIG_SECRET, ALLOW_PRIVATE_AI_URL, ASYNQ_ENABLED

# 3. Run backend server (auto-runs migrations/ on startup, loads .env via config.Load())
go run ./cmd/gofile
```

---

## 🧪 Testing & CI/CD

The project comes with a comprehensive test suite and multi-stage GitHub Actions CI:

```bash
# 1. Run all backend unit & race detector tests
go test -count=1 -race ./...

# 2. Run frontend type check and Vitest tests
cd web && npm run build && npm test && cd ..

# 3. Run performance benchmarks
go test ./internal/common/hash -bench . -benchmem -run '^$'
go test ./internal/infrastructure/storage -bench . -benchmem -run '^$'
```

---

## 📡 API Reference

Unified response format: `{"code": 0|errorCode, "msg": "...", "data": ...}` (`code = 0` indicates success).

### 1. Files & VFS Folder Operations (`/file`)

| Method | Route | Parameters / Body | Description |
|---|---|---|---|
| `POST` | `/file/upload` | `file` (multipart) | Standard file upload / fast-upload dedup |
| `GET` | `/file/meta` | `?filehash=` | Query file metadata (includes AI summary and tags) |
| `GET` | `/file/query` | `?parent_id=0&page=1&size=20` | Query VFS folder contents with breadcrumb path navigation |
| `POST` | `/file/folder/create` | `{"name":"Docs","parent_id":0}` | Create a virtual folder |
| `POST` | `/file/folder/rename` | `{"file_id":1,"new_name":"NewDocs"}` | Rename folder or file (cascades path updates to subtrees) |
| `POST` | `/file/folder/move` | `{"file_id":1,"target_parent_id":2}` | Move folder or file (**includes anti-circular move prevention**) |
| `GET` | `/file/download` | `?filehash=` (supports `Range: bytes=0-1024`) | Streaming download with HTTP Range 206 resume |
| `GET` | `/file/preview` | `?filehash=` | Online preview for images/PDF/video/text/code |
| `POST` | `/file/delete` | `filehash` (Form) | Soft delete file to recycle bin |
| `GET` | `/file/trash` | `?page=1&size=20` | Query recycle bin files |
| `POST` | `/file/restore` | `filehash` (Form) | Restore file from recycle bin (triggers AI reindexing) |
| `POST` | `/file/purge` | `filehash` (Form) | Permanently delete (cascades to storage and index on zero references) |

### 2. S3 Multipart Direct Upload (`/file/upload/multipart`)

| Method | Route | Parameters / Body | Description |
|---|---|---|---|
| `POST` | `/file/upload/multipart/init` | `{"filehash":"...","filename":"a.zip","filesize":104857600}` | Initiate S3 multipart session and issue presigned PUT URLs |
| `POST` | `/file/upload/multipart/complete` | `{"upload_id":"...","parts":[{"part_number":1,"etag":"..."}]}` | Trigger atomic server-side merge on MinIO |
| `POST` | `/file/upload/multipart/abort` | `{"upload_id":"..."}` | Abort multipart session and clean up storage parts |

### 3. AI Semantic Search & Config (`/file/ai` & `/ai/config`)

| Method | Route | Parameters / Body | Description |
|---|---|---|---|
| `GET` | `/file/ai/search` | `?q=architecture documents from past 3 days` | Multi-dimensional natural language hybrid search (RRF) |
| `GET` | `/file/ai/similar` | `?filehash=...&limit=5` | Find semantically similar documents |
| `GET` | `/file/ai/duplicates` | `?filehash=...&threshold=0.9` | Detect near-duplicate documents |
| `GET/POST/DELETE` | `/ai/config` | `{"base_url":"...","api_key":"...","model":"..."}` | Manage custom OpenAI-compatible provider per user |
| `POST` | `/ai/config/test` | `{"base_url":"...","api_key":"..."}` | Test custom AI provider connection |

---

## 📁 Project Directory Structure

```
gofile/
├── cmd/gofile/                    # Process entrypoint and signal handling
├── internal/
│   ├── app/                       # Composition root and resource lifecycle
│   ├── config/                    # Environment configuration loader
│   ├── domain/                    # Domain models and invariants
│   ├── application/service/       # File, user, share and AI use cases
│   ├── port/                      # Cross-layer contracts and ports
│   ├── transport/http/            # HTTP router and middleware
│   │   └── handler/               # HTTP controllers and cleanup hooks
│   ├── infrastructure/            # Concrete infrastructure adapters
│   │   ├── persistence/mysql/     # GORM connection and migrations
│   │   ├── persistence/repository/ # MySQL repositories
│   │   ├── storage/               # MinIO S3 and local disk adapters
│   │   ├── cache/redis/           # Redis cache, locks and rate limiting
│   │   ├── queue/asynq/            # Asynq task client and server
│   │   └── ai/                    # LLM, extraction and Typesense adapters
│   ├── job/                       # Cancellable background jobs
│   ├── common/                    # Hash, crypto, URL and chunk primitives
│   └── observability/metrics/     # Prometheus metrics and request tracing
├── docker/                        # Dockerfile, Compose and monitoring config
├── migrations/                    # golang-migrate versioned SQL scripts
└── web/                           # Frontend SPA (Vue 3 + Vite 6 + TypeScript 5 + Vitest)
    └── src/
        ├── components/           # 14 Strict TypeScript Vue components (<script setup lang="ts">)
        ├── api.ts                # Strongly-typed API client contract
        └── utils.ts & toast.ts   # Utilities & global toast alerts
```

---

## 📄 License

Apache License 2.0 © 2026 [jay77721](https://github.com/jay77721) — see [LICENSE](LICENSE) for details.
