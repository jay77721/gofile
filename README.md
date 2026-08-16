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
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](#-license)

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

* **Layered Modular Architecture**: Strict `Handler → Service → Repository/Storage/Task` dependency injection, eliminating monolithic files.
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
           │     Handler Layer      │                                   │     Service Layer      │
           │ • handler.go (CRUD)    │                                   │ • file_service.go      │
           │ • vfs.go (Folders)     │──────────────────────────────────▶│ • vfs_service.go (VFS) │
           │ • multipart.go (S3)    │                                   │ • multipart_service.go │
           │ • download.go (Range)  │                                   │ • trash_service.go     │
           └────────────────────────┘                                   └───────────┬────────────┘
                                                                                     │
                       ┌─────────────────────────────────────────────────────────────┼─────────────────────────────┐
                       ▼                                                             ▼                             ▼
           ┌────────────────────────┐                                   ┌────────────────────────┐    ┌────────────────────────┐
           │    Repository Layer    │                                   │     Storage Layer      │    │  Async Task Hub (M3)   │
           │ GORM + MySQL 8.0       │                                   │ MinIO (S3) ⇄ Local     │    │ Asynq Redis Queue      │
           │ • tbl_file (Dedup)     │                                   │ • S3 Multipart Direct  │    │ (Worker Pool / Retry)  │
           │ • tbl_user_file (VFS)  │                                   │ • HTTP Range 206       │    │ (Fallback In-Mem Chan) │
           │ • tbl_multipart_upload │                                   └────────────────────────┘    └───────────┬────────────┘
           └────────────────────────┘                                                                              │
                       │                                                                                           ▼
                       ▼                                                                              ┌────────────────────────┐
           ┌────────────────────────┐                                                                 │      AI Pipeline       │
           │         Redis          │                                                                 │ • Text Extraction      │
           │ Fast-upload/Lock/Limit │                                                                 │ • LLM Summary & Tags   │
           └────────────────────────┘                                                                 │ • Typesense Vector KNN │
                                                                                                      └────────────────────────┘
```

---

## 🚀 Quick Start

### Option 1: Docker Compose (Recommended)

```bash
git clone https://github.com/jay77721/gofile.git
cd gofile
docker compose up -d
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
cp .env.example .env

# 3. Run backend server (auto-runs migrations/ on startup)
go run main.go
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
go test ./util/ -bench . -benchmem -run '^$'
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
├── main.go                       # Entrypoint: Dependency injection & graceful shutdown
├── Dockerfile & docker-compose.yml # Container orchestration (MySQL, MinIO, Redis, Typesense, Prometheus, Grafana)
├── migrations/                   # golang-migrate versioned SQL migration scripts
├── config/                       # Environment configuration loader
├── model/                        # Domain models (File, UserFile, Multipart, User, Token, AI)
├── repository/                   # GORM repository implementations & mocks
├── service/                      # Domain services (single responsibility)
│   ├── file_service.go            # Core file CRUD & download streams
│   ├── vfs_service.go             # Virtual folder tree, materialized paths, anti-circular moves
│   ├── multipart_service.go       # S3 direct multipart & chunk merge
│   ├── trash_service.go           # Recycle bin lifecycle & cascading purge
│   ├── user_service.go            # User management
│   ├── auth_service.go            # Session & token verification
│   ├── share_service.go           # File sharing with optional passwords
│   └── ai_service.go              # AI semantic search & recommendations
├── handler/                      # HTTP controllers (high cohesion)
│   ├── handler.go                 # Core file routes & healthcheck
│   ├── vfs.go                     # VFS folder routes
│   ├── multipart.go               # S3 multipart upload routes
│   ├── download.go                # Range 206 byte parsing, download & preview
│   ├── user.go & auth.go          # Auth handlers & middleware
│   ├── share.go                   # Sharing endpoints
│   ├── ai.go & ai_config.go       # AI search & custom provider endpoints
│   ├── cleanup.go                 # Background GC workers
│   └── ratelimit.go & errcode.go  # Distributed rate limiter & unified error codes
├── task/                         # Asynq distributed task hub (Client / Server / Processor)
├── ai/                           # AI engine: LLM extraction / NLP query parser / Typesense vector index
├── storage/                      # Storage drivers (MinIO S3 multipart ⇄ Local disk)
├── cache/                        # Redis distributed locks / rate limiting / fast-upload cache
├── metrics/                      # Prometheus metrics & X-Request-ID tracing
└── web/                          # Frontend SPA (Vue 3 + Vite 6 + TypeScript 5 + Vitest)
    └── src/
        ├── components/           # 14 Strict TypeScript Vue components (<script setup lang="ts">)
        ├── api.ts                # Strongly-typed API client contract
        └── utils.ts & toast.ts   # Utilities & global toast alerts
```

---

## 📄 License

MIT © [jay77721](https://github.com/jay77721)
