# AGENTS.md

This file is the repository-local guide for coding agents working on gofile.
Higher-level instructions supplied by the user or execution environment take
precedence. Keep this file aligned with the actual repository; do not document
planned features as if they were implemented.

## 1. Project at a glance

gofile is a self-hosted, multi-user cloud-file service written in Go. The
backend provides file upload/download/preview, SHA-1 based physical
deduplication, virtual folders, recycle-bin lifecycle, sharing, S3 multipart
upload, AI metadata generation, semantic search, and Prometheus observability.

| Area | Implementation |
|---|---|
| HTTP | Go 1.25, Gin 1.12 |
| Persistence | GORM 1.31, MySQL 8, golang-migrate |
| Object storage | MinIO/S3 with local-disk fallback |
| Cache and coordination | go-redis v9: dedup cache, locks, rate limiting |
| Async processing | Asynq over Redis, with an in-process channel fallback |
| AI/search | Mock, OpenAI-compatible, or Anthropic provider; Typesense hybrid search |
| Frontend | Vue 3, TypeScript, Vite, Vitest under web/ |
| Delivery | Multi-stage Docker build, Compose, GitHub Actions |

The supported Go entrypoint is cmd/gofile. There is intentionally no root
main.go.

## 2. Repository layout

    cmd/gofile/                         process entrypoint and signal handling
    internal/
      app/                              composition root and lifecycle ownership
      application/service/              application use cases
      common/                           hash, crypto, URL and chunk primitives
      config/                           environment configuration
      domain/                           domain models and invariants
      infrastructure/
        ai/                             providers, extraction and Typesense adapter
        cache/redis/                    Redis cache, locks and rate limiting
        persistence/mysql/              SQL connection and migrations
        persistence/repository/         MySQL/GORM repositories
        queue/asynq/                    Asynq client, server and task handler
        storage/                        MinIO/S3 and local storage adapters
      job/                              cancellable periodic/compensation jobs
      observability/metrics/            Prometheus metrics and request IDs
      port/                             application-owned dependency contracts
      transport/http/                   router, middleware and HTTP handlers
    docker/                             Dockerfile, Compose and monitoring config
    docs/                               generated/API documentation
    migrations/                         versioned database migrations
    web/                                frontend source and build configuration

## 3. Commands from the repository root

### Backend

    # Load local configuration if needed
    cp .env.example .env                 # PowerShell: Copy-Item .env.example .env

    # Run the service; startup performs database migration
    go run ./cmd/gofile

    # Build the service binary
    go build -o ./bin/gofile-server ./cmd/gofile

MYSQL_DSN must point to a reachable MySQL instance. Redis, MinIO, Typesense,
AI, and Asynq are optional according to configuration; the application is
expected to degrade gracefully when optional dependencies are unavailable.

### Frontend

    npm --prefix web ci
    npm --prefix web run build

The HTTP server serves web/dist from /static. Build the frontend before running
the backend when the checked-out tree does not contain generated assets.

### Verification

Run the focused checks relevant to the change, then the complete gate before a
commit:

    go test -count=1 ./...
    go test -race ./...
    go vet ./...
    go build -o ./bin/gofile-server ./cmd/gofile

    # Optional performance checks
    go test ./internal/common/hash -bench . -benchmem -run '^$'
    go test ./internal/infrastructure/storage -bench . -benchmem -run '^$'

### Docker

    docker compose -f docker/docker-compose.yml config
    docker compose -f docker/docker-compose.yml up -d
    docker compose -f docker/docker-compose.yml ps
    docker compose -f docker/docker-compose.yml logs -f app

    # Build from the repository root; the Dockerfile is under docker/
    docker build -f docker/Dockerfile -t gofile:local .

The image build target is ./cmd/gofile. If the build environment cannot reach
proxy.golang.org, pass a reachable module proxy, for example:

    docker build -f docker/Dockerfile \
      --build-arg GOPROXY=https://goproxy.cn,direct \
      -t gofile:local .

## 4. Architecture and dependency rules

The composition root is internal/app. The intended dependency direction is:

    cmd/gofile
        -> internal/app
            -> transport/http, application/service, infrastructure, job

    transport/http -> application/service -> port -> domain
    infrastructure  -> port and domain
    job             -> port/domain and the concrete AI processor where required

Rules:

1. Keep business use cases in internal/application/service; handlers should
   translate HTTP input/output and must not contain SQL or storage workflows.
2. New service dependencies must be expressed in internal/port. Concrete
   repository, Redis, MinIO, Typesense, and Asynq types belong in infrastructure
   or the composition root.
3. Existing AI query parsing/provider construction is a compatibility seam in
   the AI services. Do not add more concrete infrastructure dependencies there;
   extract a port or application-owned adapter when expanding that area.
4. internal/app owns construction and shutdown. Constructors should receive
   dependencies explicitly; do not introduce new mutable package globals.
5. Optional dependencies must be nil-safe and must not make the core upload,
   download, metadata, or authentication path unavailable.
6. Keep domain packages independent of Gin, GORM, Redis, MinIO, Typesense, and
   other infrastructure libraries.

## 5. Non-negotiable business invariants

### Ownership and deduplication

- tbl_file is the global physical-file record, keyed by SHA-1.
- tbl_user_file stores the user-facing name, ownership, VFS parent/path, and
  soft-delete state.
- Every user file operation must verify the user relationship, normally through
  fileRepo.GetByHash(ctx, filehash, username) or an equivalent scoped query.
- Never let a file hash alone bypass ownership checks. Unauthorized access maps
  to business code 1003.

### Storage and consistency

- The database is the metadata source of truth; object storage and Typesense are
  external/derived systems.
- If metadata persistence fails after an object upload, perform compensating
  storage deletion where the workflow permits it.
- Local storage publishes through a temporary file followed by an atomic rename.
- S3 multipart uploads validate the upload session, part ordering, ETags/parts,
  final size, and final SHA-1 before metadata is completed.
- Do not put upload data in the repository, root temporary paths, or untracked
  ad-hoc directories. Use configured storage/chunk directories.

### VFS and deletion

- VFS paths use materialized dir_path values for subtree queries.
- Folder moves must reject self-descendant/circular moves and preserve user
  ownership and status constraints.
- Soft delete changes the user relationship state. Physical deletion is allowed
  only after checking that no active user references remain.
- Background cleanup must be cancellable and must not close dependencies before
  all workers have stopped.

### Async AI processing

- AI work is asynchronous and must not block the successful upload response.
- Asynq is the durable/cross-instance path when enabled; the in-process channel
  is the explicit fallback and may lose queued work on process restart.
- Tasks are idempotent by user/file identity and use bounded retry/compensation
  behavior. Errors must be logged with structured context.
- AI/Typesense failure falls back to the non-AI file path or LIKE search where
  that behavior already exists.

## 6. HTTP contract

Successful responses use {"code": 0, "msg": "...", "data": ...}. Handlers
should use respondError(c, httpStatus, code, msg) for failures. HTTP status
and business code are intentionally separate.

| Code | Meaning |
|---:|---|
| 1001 | Invalid parameters |
| 1002 | Unauthorized/not logged in |
| 1003 | Forbidden/not owner |
| 1004 | Resource not found |
| 1005 | User already exists |
| 1006 | Invalid credentials |
| 1007 | Upload failed |
| 1008 | Merge failed |
| 1009 | Storage operation failed |
| 1010 | Rate limited |
| 1011 | Search failed |
| 1099 | Internal error |

Main endpoint groups:

- /user: signup, signin, logout, info
- /file: upload, metadata, query, download, preview, update, delete
- /file/trash: list, restore, purge
- /file/upload/chunk and /file/upload/multipart/*: resumable and S3 uploads
- /file/folder: create, rename, move
- /file/share and /share/:token: create, list, revoke, public download
- /file/ai and /ai/config: semantic search, recommendations, duplicates, and
  user provider configuration
- /healthz and /metrics: health and Prometheus endpoints

## 7. Change workflow

1. Start with git status --short and inspect the relevant package/tests.
   Preserve unrelated user changes, including untracked files.
2. Define the owning package and keep the write scope narrow. Do not move or
   delete files merely to make a diff look cleaner.
3. Use apply_patch for source/document edits. Run gofmt on changed Go files.
4. Add or update tests for behavior, error paths, cancellation, ownership, and
   concurrency when relevant. Prefer injected fakes over external services in
   unit tests.
5. Run focused tests, then the full verification gate in section 3. A change is
   not ready if it only passes a package-local test while the repository does
   not build.
6. Review git diff --check, the staged diff, and the final status before
   committing. If the user asks for GitHub delivery, push each validated,
   coherent stage; never push secrets or unrelated work.

## 8. Boundaries and prohibitions

- Do not rewrite committed migration files under migrations/; add the next
  numbered migration instead.
- Do not hand-edit generated Swagger files under docs/; regenerate them with
  the project's Swagger command when required.
- Never commit .env, credentials, API keys, private keys, generated uploads,
  chunks, or build artifacts.
- Do not use destructive Git commands such as reset --hard or checkout -- to
  discard work. Ask before removing material outside the explicitly requested
  scope.
- Keep changes backwards-compatible at the HTTP/API and port boundaries unless
  the task explicitly requests a breaking change.
- Do not claim RAG chat, WebDAV, or other roadmap items are implemented until
  code, tests, and deployment support actually exist.

## 9. Current status and roadmap

Implemented in the current main branch:

- Modular Go layout with cmd/gofile entrypoint and internal/ boundaries.
- Port-based application service dependencies and constructor-based wiring.
- SHA-1 fast upload/deduplication, resumable chunks, S3 multipart, Range 206,
  VFS operations, recycle bin, sharing, and ownership isolation.
- Redis cache/locks/rate limiting with graceful fallback behavior.
- AI extraction, provider selection, Typesense hybrid search, and asynchronous
  task processing.
- Explicit resource lifecycle for MySQL, HTTP, AI, Asynq, cache, and background
  cleanup jobs.
- Docker/Compose deployment and CI build paths using ./cmd/gofile.

Not implemented; treat as future work only:

- RAG document Q&A with chunk retrieval and SSE streaming.
- WebDAV protocol support.
- A repository-wide coverage target of 80% or higher.
