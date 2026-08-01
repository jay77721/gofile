# gofile

> Gin + MinIO + MySQL

A lightweight file storage server built with Go, supporting file upload/download, chunked upload with resumable capability, user authentication, and hash-based instant upload deduplication.

[中文](#中文) | [MIT License](LICENSE)

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
# Edit .env to match your setup

# 3. Initialize database
mysql -u root -p gofile < schema.sql

# 4. Run
go run main.go
```

Or use the startup scripts (loads `.env` automatically):

```bash
./start.sh              # Start with .env
./start.sh --migrate    # Run schema.sql then start
./start.sh --build      # Build binary then run
```

On Windows:

```cmd
start.bat              # Start with .env
start.bat --migrate    # Run schema.sql then start
start.bat --build      # Build binary then run
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
└── docker-compose.yml      # Docker Compose orchestration
```

## Testing

```bash
go test ./...           # Run all tests
go test ./util/...      # Run util tests only
go test ./handler/...   # Run handler tests only
```

Tests cover:
- `util/` — SHA1, MD5, file operations, path utilities
- `handler/` — HTTP responses, status codes, JSON format, auth middleware, rate limiting, user handler tests, edge cases

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

---

## 中文

> Gin + MinIO + MySQL

基于 Go 的轻量级网盘服务，支持文件上传/下载、分片上传与断点续传、用户认证、基于 hash 的秒传去重。

[English](#gofile) | [MIT License](LICENSE)

### 功能特性

- **文件管理** — 上传、下载、查询、重命名、软删除（100MB 大小限制）
- **分片上传** — 大文件分片、断点续传、幂等重试、自动合并、基于 hash 的秒传去重
- **用户认证** — 注册/登录，bcrypt 密码哈希，Cookie 会话（HttpOnly），24h Token 自动过期
- **存储后端** — 优先 MinIO（S3 兼容），初始化失败时自动回退到本地文件存储，通过 `storage.Storage` 接口抽象
- **文件所有权** — 每个文件关联上传者，下载/查询/重命名操作均校验归属
- **安全防护** — 路径穿越防护、IP 令牌桶限流（5 req/s，burst 10）、可信代理配置、RFC 5987 Content-Disposition 编码、输入校验、安全随机 Token
- **可观测性** — 结构化 JSON 日志（log/slog）、健康检查端点、优雅关闭、定时清理过期分片

### 快速开始

#### Docker 部署（推荐）

```bash
git clone git@github.com:jay77721/gofile.git
cd gofile
docker compose up -d
```

服务启动后访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| 应用 | http://localhost:8080 | 主服务 |
| MinIO 控制台 | http://localhost:9001 | 用户名/密码：`minioadmin` / `minioadmin` |

MySQL、MinIO 均由 Docker Compose 自动初始化与配置。

#### 手动部署

**环境要求：** Go 1.25+、MySQL、MinIO（可选）

```bash
# 1. 安装依赖
go mod tidy

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env 以匹配你的配置

# 3. 初始化数据库
mysql -u root -p gofile < schema.sql

# 4. 启动
go run main.go
```

或使用启动脚本（自动加载 `.env`）：

```bash
./start.sh              # 使用 .env 启动
./start.sh --migrate    # 运行 schema.sql 后启动
./start.sh --build      # 构建二进制后运行
```

Windows 用户：

```cmd
start.bat              # 使用 .env 启动
start.bat --migrate    # 运行 schema.sql 后启动
start.bat --build      # 构建二进制后运行
```

### 配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `SERVER_ADDR` | `:8080` | HTTP 监听地址 |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?...` | MySQL 连接字符串 |
| `UPLOAD_DIR` | `./uploads` | 本地文件存储目录（fallback） |
| `CHUNK_DIR` | `./chunks` | 分片临时目录 |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO 服务地址（空=跳过 MinIO） |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO Access Key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO Secret Key |
| `MINIO_BUCKET` | `filestore` | MinIO 存储桶名称 |
| `MINIO_USE_SSL` | `false` | 是否启用 SSL |

> 服务会优先尝试连接 MinIO。若 `MINIO_ENDPOINT` 为空或 MinIO 初始化失败，自动回退到 `UPLOAD_DIR` 本地存储。

### API 接口

#### 文件操作（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/upload` | 上传文件（支持秒传） |
| `GET` | `/file/meta` | 按 hash 获取文件元数据 |
| `GET` | `/file/query` | 查询当前用户所有文件 |
| `GET` | `/file/download` | 按 hash 下载文件 |
| `POST` | `/file/update` | 重命名文件 |
| `POST` | `/file/delete` | 软删除文件 |

#### 分片上传（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/upload/chunk` | 上传单个分片（幂等） |
| `GET` | `/file/upload/status` | 查询已上传的分片索引 |
| `POST` | `/file/upload/merge` | 合并分片为完整文件 |

#### 用户操作

| 方法 | 路由 | 需认证 | 限流 | 说明 |
|------|------|:------:|:----:|------|
| `POST` | `/user/signup` | × | ✓ | 注册 |
| `POST` | `/user/signin` | × | ✓ | 登录，Token 仅通过 HttpOnly Cookie 返回 |
| `GET` | `/user/info` | ✓ | × | 获取用户信息 |

#### 系统

| 方法 | 路由 | 说明 |
|------|------|------|
| `GET` | `/healthz` | 健康检查 |
| `GET` | `/static/*` | 静态前端页面 |

### 架构

#### 存储层

通过 `storage.Storage` 接口（Put/Get/Exists/Delete）支持两级存储后端：

- **MinIO** — S3 兼容对象存储，Docker 部署时自动启动，生产推荐
- **Local** — 本地文件系统，MinIO 不可用时自动回退

接口在启动时通过 `handler.InitStore()` 注入。MySQL 存储元数据，文件内容存储在存储后端。

#### 文件所有权

每个文件关联上传者的用户名。以下操作已限定为当前用户：

- **下载** — 仅文件所有者可下载
- **查询** — 仅返回当前用户的文件
- **重命名** — 仅文件所有者可操作
- **删除** — 软删除（status=2，文件内容保留在存储中）

#### 分片上传流程

1. 客户端将文件切分为多个分片
2. 逐个 POST `/file/upload/chunk`（幂等，重试同一分片安全）
3. 客户端 GET `/file/upload/status` 查询已上传分片
4. 全部上传完成后 POST `/file/upload/merge` 触发合并
5. 服务端按顺序拼接分片为完整文件，落盘到存储后端
6. 清理临时分片文件及目录
7. 后台定时任务清理过期分片目录（1h 间隔，24h 最长保留）

### 项目结构

```
gofile/
├── main.go                 # 入口、路由注册、优雅关闭
├── schema.sql              # 数据库建表脚本（含索引，单文件）
├── config/
│   └── config.go           # 环境变量配置
├── db/
│   ├── mysql/
│   │   └── conn.go         # MySQL 连接池
│   ├── file.go             # tbl_file CRUD + FileMeta 领域模型
│   └── user.go             # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go          # 文件上传/下载/查询/删除 + 分片上传
│   ├── user.go             # 注册/登录 + bcrypt + token 生成
│   ├── auth.go             # 认证中间件（Cookie session）
│   ├── ratelimit.go        # IP 限流中间件
│   └── cleanup.go          # 定时清理过期分片
├── storage/
│   ├── storage.go          # Storage 接口定义
│   ├── minio.go            # MinIO 对象存储实现
│   └── local.go            # 本地文件存储实现
├── util/
│   ├── hash.go             # SHA1、MD5、文件哈希、路径工具
│   └── chunk.go            # 磁盘-based 分片追踪
├── static/                 # 前端 HTML 页面
├── start.sh                # Unix/macOS 启动脚本
├── start.bat               # Windows 启动脚本
├── .env.example            # 环境变量模板
├── Dockerfile              # 多阶段构建
└── docker-compose.yml      # Docker Compose 编排
```

### 测试

```bash
go test ./...           # 运行全部测试
go test ./util/...      # 工具包测试
go test ./handler/...   # Handler 测试
```

测试覆盖：
- `util/` — SHA1、MD5、文件操作、路径工具
- `handler/` — HTTP 响应、状态码、JSON 格式、认证中间件、限流、用户 handler 测试、边界情况

### 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | Gin | 高性能，社区活跃 |
| 存储 | MySQL + MinIO | 关系库 + 对象存储（MinIO + 本地 fallback） |
| 认证 | bcrypt + Cookie/Session | 密码哈希，Token 会话 |
| 日志 | log/slog | 结构化 JSON 输出 |
| 部署 | Docker Compose | 一键启动所有依赖 |

### 许可证

MIT