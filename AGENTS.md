# AGENTS.md

> AI Assistant & Agent Developer Guide (Single Source of Truth)

## 1. Project Overview & Tech Stack

- **Description:** gofile is a lightweight, high-performance self-hosted cloud storage service with AI semantic search.
- **Backend:** Go 1.25, Gin 1.12, GORM 1.31 + MySQL (migrations managed via `golang-migrate`)
- **Storage:** `storage.Storage` interface — MinIO (S3) prioritized, local disk fallback (atomic write)
- **Cache & Locks:** go-redis v9 (fast-upload cache, distributed locks, rate limiting)
- **Async Queue:** `hibiken/asynq` (M3 distributed task scheduling; in-memory chan fallback)
- **AI & Search:** Typesense (hybrid full-text + vector KNN search), LLM Provider (mock/openai/anthropic)
- **Frontend:** Vue 3 + Vite + TypeScript (`web/`, build output in `web/dist`)

---

## 2. Essential Commands

### Build & Run
```bash
# 1. Frontend Build (Required: /static route serves web/dist)
cd web && npm ci && npm run build && cd ..

# 2. Run Backend (Auto-migrates MySQL schema on start)
cp .env.example .env
go run main.go               # Or: go build -o gofile . && ./gofile

# 3. Docker Compose
docker compose up -d
```

### Test & Verification (MUST pass before every commit)
```bash
# Run all tests with race detector
go test -race ./...

# Benchmark performance
go test ./util/ -bench . -benchmem -run '^$'
```

---

## 3. Core Architecture & Invariants (DO NOT BREAK)

1. **Strict 3-Tier Layered Architecture & Dependency Injection**:
   - `handler → service → repository + storage/cache/ai/task`. Strict downward dependency only.
   - All components instantiated via constructors (`NewXxxService(...)`). **No package-level global variables.**
   - Optional components (Redis, AI, Typesense, Asynq) must be nil-safe with automatic graceful degradation.

2. **File Ownership Model (Core Invariant)**:
   - `tbl_file` deduplicates globally by SHA1; `tbl_user_file` tracks user ownership and VFS tree paths.
   - **EVERY** file operation must verify ownership via `fileRepo.GetByHash(filehash, username)`. Return `1003 Forbidden` on unauthorized access.

3. **Storage & Data Integrity**:
   - **S3 Multipart Direct Upload**: Clients stream chunks directly to MinIO; server triggers atomic merge on MinIO (zero local disk I/O).
   - If database insertion fails after storage upload, **MUST** trigger compensatory rollback (`store.Delete(ctx, key)`).

4. **VFS (Virtual File System)**:
   - Uses Materialized Path (`dir_path`, e.g. `/Go/Sources/`) for high-performance subtree queries.
   - Circular Move Prevention: Moving a folder into its own descendant folder is strictly prohibited.
   - Folder rename/move updates child paths atomically using SQL `CONCAT` + `SUBSTRING`.

5. **Asynq Task Scheduling (M3)**:
   - `ASYNQ_ENABLED=true` persists AI tasks to Redis with worker pool, MaxRetry=3 backoff, and dead-letter queue.
   - TaskID format is `username:filehash` (idempotent deduplication).
   - Seamlessly falls back to internal memory channel when Redis is unavailable.

---

## 4. Development & Coding Conventions

### Git Commit Convention (STRICT: English Only)
**All Git commit messages MUST be written strictly in English using Conventional Commits**:
- `feat(scope): add new feature` (e.g. `feat(task): implement asynq worker pool`)
- `fix(scope): fix bug` (e.g. `fix(service): trigger ai enqueue on fast upload`)
- `docs: update documentation` (e.g. `docs: update AGENTS.md template`)
- `refactor(scope): refactor code without behavior changes`
- `test(scope): add or update unit/integration tests`
- `chore/build/ci: build, dependencies, or CI updates`

### Code Cleanliness & Zero-Redundancy (Keep It Simple)
- **Idiomatic Go**: Keep code flat, explicit, and direct. Avoid over-engineering and unnecessary wrapper structs.
- **Single Source of Truth (DRY)**: Never duplicate business logic, validation, or queries across layers.
- **Explicit Error Handling**: Always check errors explicitly. Use `slog.*Context` with structured metadata (automatically includes `request_id`).

### Unified Error Codes & Response Format
- JSON Format: `gin.H{"code": 0|errorCode, "msg": "...", "data": ...}` (`code = 0` on success).
- Handlers MUST use `respondError(c, httpStatus, code, msg)`.
- Error codes: `1001` Params / `1002` Unauthorized / `1003` Forbidden / `1004` NotFound / `1005` UserExists / `1006` InvalidCreds / `1007` UploadFailed / `1008` MergeFailed / `1009` StorageError / `1010` TooManyRequests / `1011` SearchFailed / `1099` InternalError.

---

## 5. Boundaries & Prohibitions (What NOT to do)

- ❌ **DO NOT** modify historical migration files (`migrations/*.sql`) already committed. Add new numbered migrations instead.
- ❌ **DO NOT** edit auto-generated Swagger artifacts (`docs/docs.go`, `docs/swagger.*`). Run `swag init` instead.
- ❌ **DO NOT** write user upload files directly to root or untracked temporary paths. Use configured storage providers.
- ❌ **DO NOT** commit secrets, private keys, or plain credentials. Use environment variables.

---

## 6. Key API Endpoints Reference

- **Auth (`/user`)**: `POST /signup`, `POST /signin`, `POST /logout`, `GET /info`
- **File Core (`/file`)**: `POST /upload`, `GET /meta`, `GET /query` (supports `parent_id` & paging), `GET /download` (Range 206), `POST /update`, `POST /delete`
- **Recycle Bin (`/file`)**: `GET /trash`, `POST /restore`, `POST /purge`
- **S3 Multipart (`/file/upload/multipart`)**: `POST /init`, `POST /complete`, `POST /abort`
- **VFS Folder (`/file/folder`)**: `POST /create`, `POST /rename`, `POST /move`
- **AI Features (`/file/ai` & `/ai/config`)**: `GET /search`, `GET /similar`, `GET /duplicates`, `GET|POST|DELETE /ai/config`, `POST /ai/config/test`
- **Share (`/file/share` & `/share/:token`)**: `POST /share`, `GET /share/list`, `POST /share/revoke`, `GET /share/:token?pwd=`

---

## 7. Known Issues & Roadmap Status

### Completed
- [x] P0: Bug fixes + GORM migration
- [x] P1: Redis fast-upload cache, distributed locks, rate limiting
- [x] P2: S3 Presigned direct upload & download URLs
- [x] P3: HTTP Range 206 resumable download & pagination
- [x] P4: Prometheus metrics & slog request_id tracing
- [x] P6: Typesense hybrid AI search, NLP query parser, auto-tagging
- [x] M1: S3 Multipart direct upload & server-side merge (`c9d150f`)
- [x] M2: VFS tree directory with Materialized Path & circular move checks (`c9d150f`)
- [x] M3: Asynq distributed task scheduling with dual-path fallback (`50d7309`)

### Known Issues & Next Milestones
- 🟡 `web/dist` is not committed: run `npm run build` in `web/` before running server.
- ⏳ M4: Drive RAG Q&A (document chunking + SSE stream chat).
- ⏳ M5: WebDAV protocol support (`golang.org/x/net/webdav`).
- ⏳ P5.2: Unit test coverage push to ≥80%.
