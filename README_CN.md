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

**轻量级自建网盘 | Lightweight Self-hosted File Storage**

[English](README.md) · [中文](#功能特性) · [快速开始](#快速开始) · [API 接口](#api-接口) · [项目结构](#项目结构)

</div>

---

## ✨ 功能特性

| | 功能 | 说明 |
|---|------|------|
| 📤 | **文件上传** | 普通上传 + 秒传去重（SHA1 hash + Redis 缓存加速） |
| 📥 | **文件下载** | HTTP Range 断点续传（206 Partial Content） |
| ⚡ | **预签名直传** | 基于 S3 预签名 URL，客户端直接和 MinIO 通信，应用服务器零字节拷贝 |
| ✂️ | **分片上传** | 大文件切片上传，断点续传，幂等重试，自动合并 |
| 🔐 | **用户认证** | bcrypt 密码哈希 + HttpOnly Cookie Session |
| 👤 | **文件归属** | `tbl_user_file` 关联表设计，秒传时每个用户独立拥有文件 |
| 🔒 | **分布式锁** | Redis SETNX + Lua CAS，解决并发分片合并的临时文件覆盖竞态 |
| ⏱️ | **全局限流** | Redis 固定窗口计数器，支持多实例共享限流 |
| 🛡️ | **安全防护** | 路径穿越防护（40位hex校验）、chunk 用户隔离、RFC 5987 编码 |
| ☁️ | **存储后端** | MinIO (S3) 优先，失败自动回退本地磁盘 |
| 🧹 | **自动清理** | 过期分片定时清理（1h 间隔）+ 软删除文件垃圾回收（24h 间隔） |
| 📊 | **结构化日志** | JSON 格式 log/slog，便于收集分析 |

---

## 🏗️ 架构

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

## 🚀 快速开始

### 方式一：Docker Compose（推荐）

```bash
git clone git@github.com:jay77721/gofile.git
cd gofile
docker compose up -d
```

启动后访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| 🌐 应用 | http://localhost:8080 | 主服务 |
| 🗄️ MinIO | http://localhost:9001 | 用户名/密码：`minioadmin` |
| 🔴 Redis | localhost:6379 | 可选，无 Redis 时应用照常工作 |

### 方式二：手动部署

```bash
# 1. 安装依赖
go mod tidy

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env 以匹配你的配置

# 3. 启动（GORM AutoMigrate 自动建表）
go run main.go

# 或手动建表：
mysql -u root -p gofile < schema.sql
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

---

## ⚙️ 配置

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
| `REDIS_ADDR` | `localhost:6379` | Redis 地址（空=跳过 Redis） |
| `REDIS_PASSWORD` | `` | Redis 密码 |
| `REDIS_DB` | `0` | Redis 数据库编号 |
| `COOKIE_SECURE` | `false` | Cookie Secure 标志（生产环境设为 true） |

> 💡 服务会优先尝试连接 MinIO。若 `MINIO_ENDPOINT` 为空或 MinIO 初始化失败，自动回退到 `UPLOAD_DIR` 本地存储。Redis 可选，所有 Redis 功能在不可用时自动降级。

---

## 📡 API 接口

### 文件操作（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/upload` | 上传文件（支持秒传） |
| `GET` | `/file/meta` | 按 hash 获取文件元数据 |
| `GET` | `/file/query` | 查询用户文件（支持分页：`?page=1&size=20`） |
| `GET` | `/file/download` | 按 hash 下载文件（支持 Range 断点续传） |
| `GET` | `/file/preview` | 在线预览（图片/PDF/视频/文本） |
| `POST` | `/file/update` | 重命名文件 |
| `POST` | `/file/delete` | 软删除文件 |

### 预签名直传（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/presigned/upload` | 获取预签名上传 URL（客户端直传 MinIO） |
| `POST` | `/file/presigned/upload/confirm` | 确认预签名上传完成 |
| `GET` | `/file/presigned/download` | 获取预签名下载 URL（客户端直下 MinIO） |

### 分片上传（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/upload/chunk` | 上传单个分片（幂等，用户隔离） |
| `GET` | `/file/upload/status` | 查询已上传的分片索引 |
| `POST` | `/file/upload/merge` | 合并分片（分布式锁，UUID 临时文件防冲突） |

### 用户操作

| 方法 | 路由 | 需认证 | 限流 | 说明 |
|------|------|:------:|:----:|------|
| `POST` | `/user/signup` | × | ✓ | 注册 |
| `POST` | `/user/signin` | × | ✓ | 登录，Token 仅通过 HttpOnly Cookie 返回 |
| `GET` | `/user/info` | ✓ | × | 获取用户信息 |

### 系统

| 方法 | 路由 | 说明 |
|------|------|------|
| `GET` | `/healthz` | 健康检查 |
| `GET` | `/metrics` | Prometheus 指标端点 |
| `GET` | `/static/*` | 静态前端页面 |

---

## 💻 使用示例

```bash
# 注册
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup

# 登录（cookie 存储在本地，后续请求自动带上）
curl -X POST -F "username=test&password=123456" http://localhost:8080/user/signin -c cookies.txt

# 上传文件
curl -X POST -F "file=@./test.txt" -b cookies.txt \
  http://localhost:8080/file/upload

# 下载文件（支持 Range 断点续传）
curl -b cookies.txt -H "Range: bytes=0-1023" \
  "http://localhost:8080/file/download?filehash=HASH" -o partial.bin

# 分页查询
curl -b cookies.txt "http://localhost:8080/file/query?page=1&size=10"

# 预签名上传（两步）
# 第 1 步：获取预签名 URL（前端先算好文件 SHA1）
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload

# 第 2 步：直接 PUT 到 MinIO（不经过应用服务器）
curl -X PUT -T ./test.txt "PRESIGNED_URL"

# 第 3 步：确认上传
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload/confirm

# 预签名下载
curl -b cookies.txt "http://localhost:8080/file/presigned/download?filehash=HASH"
```

---

## 🧪 测试

```bash
go test ./...           # 运行全部测试
go test -v ./handler/   # handler 测试（详细输出）
go test ./util/         # 工具函数测试
```

**测试覆盖：**
- `util/` — SHA1, MD5, 文件操作, 路径工具
- `handler/` — HTTP 响应, 状态码, JSON 格式, 认证中间件, 限流, 用户注册/登录验证, 边界情况
- `metrics/` — request_id 中间件/日志 ContextHandler, Prometheus 指标中间件, `/metrics` 端点
- `handler/observability_test.go` — 全链路可观测性测试（RequestID → Metrics → Recovery）

---

## 📊 可观测性

服务暴露 Prometheus 指标，并通过 request_id 跨层串联日志。

### 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `http_requests_total` | Counter | `method`, `path`, `status` | HTTP 请求计数 |
| `http_request_duration_seconds` | Histogram | `method`, `path` | 请求耗时（默认分桶） |
| `file_upload_bytes_total` | Counter | — | 上传成功的文件字节数 |

- **path 标签使用路由模板**（`c.FullPath()`），而非原始 URL —— 避免 query 参数 / 文件 hash 造成标签基数爆炸。未匹配路由回退为 `unknown`。
- 每 15s 抓取 `GET /metrics`。`docker compose up -d` 会启动 **Prometheus**（`http://localhost:9090`）和 **Grafana**（`http://localhost:3000`，admin/admin），并预置 "gofile Overview" 大盘（RPS、P95 延迟、5xx 错误率、上传字节）。

### request_id 日志串联

每个请求生成 UUID（响应头 `X-Request-ID`）并注入请求 `context`。自定义 `slog.Handler`（`metrics.ContextHandler`）从 context 提取 `request_id` 附加到每条日志 —— 因此 handler、service、访问日志可按请求串联：

```json
{"level":"INFO","msg":"access","method":"GET","path":"/file/upload","status":200,"request_id":"fd2d1cdf-..."}
{"level":"INFO","msg":"file uploaded","size":2048,"request_id":"fd2d1cdf-..."}
```

后台任务（chunk 清理、软删除 GC）故意**不带** request_id。

---

## 📁 项目结构

```
gofile/
├── main.go                 # 入口、依赖注入、路由注册、优雅关闭
├── schema.sql              # 数据库建表脚本（参考用，GORM AutoMigrate 自动建表）
├── config/
│   └── config.go           # 环境变量配置（支持 .env）
├── db/
│   └── mysql/
│       └── conn.go         # GORM 连接池 + AutoMigrate
├── model/
│   ├── file.go             # File（全局注册）+ UserFile（用户拥有）+ FileMeta（DTO）
│   ├── user.go             # User GORM 模型
│   └── token.go            # Token GORM 模型
├── repository/
│   ├── file_repo.go        # FileRepository（GORM 实现 + Mock）
│   ├── user_repo.go        # UserRepository（GORM 实现 + Mock）
│   └── token_repo.go       # TokenRepository（GORM 实现 + Mock）
├── service/
│   ├── file_service.go     # 上传/下载/合并/预签名/秒传去重业务逻辑
│   ├── user_service.go     # 注册/登录/token 生成
│   └── auth_service.go     # Token 校验
├── handler/
│   ├── handler.go          # 文件 HTTP 处理器（上传/下载/合并/预签名/Range）
│   ├── user.go             # 用户 HTTP 处理器（注册/登录/信息）
│   ├── auth.go             # 认证中间件（Cookie session）
│   ├── ratelimit.go        # 限流中间件（Redis 或内存回退）
│   ├── handler_test.go     # Handler 测试
│   ├── observability_test.go # 全链路可观测性测试
│   └── cleanup.go          # 定时清理过期分片 + 软删除文件 GC
├── metrics/
│   ├── metrics.go          # Prometheus 指标定义 + /metrics 端点
│   ├── middleware.go       # 指标中间件（计数器 + 直方图 + 访问日志）
│   ├── request_id.go       # RequestID 中间件 + slog context handler
│   ├── metrics_test.go     # 指标测试
│   └── request_id_test.go  # Request ID 测试
├── storage/
│   ├── storage.go          # Storage 接口（Put/Get/Exists/Delete/Presign/Range）
│   ├── minio.go            # MinIO S3 实现
│   └── local.go            # 本地文件系统实现
├── cache/
│   ├── cache.go            # Redis 客户端封装
│   ├── hash.go             # 文件 hash 去重缓存（Set）
│   └── lock.go             # 分布式锁（SETNX + Lua CAS）
├── util/
│   ├── hash.go             # SHA1, MD5, 文件哈希工具
│   ├── hash_test.go        # 工具函数测试
│   └── chunk.go            # 磁盘-based 分片追踪
├── static/                 # 前端 HTML（Vue 3 + Dark Mode SPA）
├── start.sh                # Unix/macOS 启动脚本
├── start.bat               # Windows 启动脚本
├── deploy/
│   ├── prometheus/         # Prometheus 抓取配置
│   └── grafana/            # Grafana 数据源 + 大盘预置
├── .env.example            # 环境变量模板
├── Dockerfile              # 多阶段 Docker 构建
├── docker-compose.yml      # Docker Compose 编排（MySQL + MinIO + Redis + Prometheus + Grafana）
├── README.md               # 项目说明文档 (EN)
└── AGENTS.md               # AI 开发协作文档
```

---

## 🔧 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| 语言 | Go 1.25 | 强类型、高并发、高性能 |
| HTTP 框架 | Gin | 高性能，社区活跃 |
| ORM | GORM | AutoMigrate 自动建表，模型标签，预编译缓存 |
| 数据库 | MySQL 8.0 | 关系型元数据存储 |
| 对象存储 | MinIO (S3) | 预签名 URL，客户端直传直下 |
| 缓存 | Redis 7（可选） | 秒传 hash 去重缓存、分布式锁、全局限流 |
| 认证 | bcrypt + Cookie/Session | 密码哈希，HttpOnly Cookie |
| 日志 | log/slog | 结构化 JSON 输出 + request_id 串联 |
| 指标 | Prometheus client_golang | 自定义 Gin 中间件，`/metrics` 端点 |
| 监控 | Prometheus + Grafana | 预置 "gofile Overview" 大盘 |
| 部署 | Docker Compose | 一键启动所有依赖服务 |

---

## 📄 许可证

MIT

---

<div align="center">

Made with ❤️ by [jay77721](https://github.com/jay77721)

</div>