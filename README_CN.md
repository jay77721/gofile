# FileStore Server

> Gin + MinIO + MySQL

基于 Go 的轻量级网盘服务，支持文件上传/下载、分片上传与断点续传、用户认证、基于 hash 的秒传去重。

[English](README.md) | [MIT License](LICENSE)

---

## 功能特性

- **文件管理** — 上传、下载、查询、重命名、软删除（100MB 大小限制）
- **分片上传** — 大文件分片、断点续传、幂等重试、自动合并、基于 hash 的秒传去重
- **用户认证** — 注册/登录，bcrypt 密码哈希，Cookie 会话（HttpOnly），24h Token 自动过期
- **存储后端** — 优先 MinIO（S3 兼容），初始化失败时自动回退到本地文件存储，通过 `storage.Storage` 接口抽象
- **文件所有权** — 每个文件关联上传者，下载/查询/重命名操作均校验归属
- **安全防护** — 路径穿越防护、IP 令牌桶限流（5 req/s，burst 10）、可信代理配置、RFC 5987 Content-Disposition 编码、输入校验、安全随机 Token
- **可观测性** — 结构化 JSON 日志（log/slog）、健康检查端点、优雅关闭、定时清理过期分片

## 快速开始

### Docker 部署（推荐）

```bash
git clone git@github.com:jay77721/FileStore-server.git
cd filestore-server
docker compose up -d
```

服务启动后访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| 应用 | http://localhost:8080 | 主服务 |
| MinIO 控制台 | http://localhost:9001 | 用户名/密码：`minioadmin` / `minioadmin` |

MySQL、MinIO 均由 Docker Compose 自动初始化与配置。

### 手动部署

**环境要求：** Go 1.25+、MySQL、MinIO（可选）

```bash
# 1. 安装依赖
go mod tidy

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env 以匹配你的配置（MYSQL_DSN、MINIO_ENDPOINT 等）

# 3. 初始化数据库
mysql -u root -p fileserver < migrations/000001_init_schema.up.sql
mysql -u root -p fileserver < migrations/000002_add_indexes.up.sql
mysql -u root -p fileserver < migrations/000003_add_file_owner.up.sql

# 4. 启动
go run main.go
```

或使用启动脚本（自动加载 `.env`）：

```bash
./scripts/start.sh              # 使用 .env 启动
./scripts/start.sh --migrate    # 运行迁移后启动
./scripts/start.sh --build      # 构建二进制后运行
```

Windows 用户：

```cmd
scripts\start.bat              # 使用 .env 启动
scripts\start.bat --migrate    # 运行迁移后启动
scripts\start.bat --build      # 构建二进制后运行
```

## 配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `SERVER_ADDR` | `:8080` | HTTP 监听地址 |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL 连接字符串 |
| `UPLOAD_DIR` | `./uploads` | 本地文件存储目录（fallback） |
| `CHUNK_DIR` | `./chunks` | 分片临时目录 |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO 服务地址（空=跳过 MinIO） |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO Access Key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO Secret Key |
| `MINIO_BUCKET` | `filestore` | MinIO 存储桶名称 |
| `MINIO_USE_SSL` | `false` | 是否启用 SSL |

> 服务会优先尝试连接 MinIO。若 `MINIO_ENDPOINT` 为空或 MinIO 初始化失败，自动回退到 `UPLOAD_DIR` 本地存储。

## API 接口

### 文件操作（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/upload` | 上传文件（支持秒传） |
| `GET` | `/file/meta` | 按 hash 获取文件元数据 |
| `GET` | `/file/query` | 查询当前用户所有文件 |
| `GET` | `/file/download` | 按 hash 下载文件 |
| `POST` | `/file/update` | 重命名文件 |
| `POST` | `/file/delete` | 软删除文件 |

### 分片上传（需认证）

| 方法 | 路由 | 说明 |
|------|------|------|
| `POST` | `/file/upload/chunk` | 上传单个分片（幂等） |
| `GET` | `/file/upload/status` | 查询已上传的分片索引 |
| `POST` | `/file/upload/merge` | 合并分片为完整文件 |

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
| `GET` | `/static/*` | 静态前端页面 |

## 架构

### 存储层

通过 `storage.Storage` 接口（Put/Get/Exists/Delete）支持两级存储后端：

- **MinIO** — S3 兼容对象存储，Docker 部署时自动启动，生产推荐
- **Local** — 本地文件系统，MinIO 不可用时自动回退

接口在启动时通过 `handler.InitStore()` 注入。MySQL 存储元数据，文件内容存储在存储后端。

### 文件所有权

每个文件关联上传者的用户名。以下操作已限定为当前用户：

- **下载** — 仅文件所有者可下载
- **查询** — 仅返回当前用户的文件
- **重命名** — 仅文件所有者可操作
- **删除** — 软删除（status=2，文件内容保留在存储中）

### 分片上传流程

1. 客户端将文件切分为多个分片
2. 逐个 POST `/file/upload/chunk`（幂等，重试同一分片安全）
3. 客户端 GET `/file/upload/status` 查询已上传分片
4. 全部上传完成后 POST `/file/upload/merge` 触发合并
5. 服务端按顺序拼接分片为完整文件，落盘到存储后端
6. 清理临时分片文件及目录
7. 后台定时任务清理过期分片目录（1h 间隔，24h 最长保留）

## 项目结构

```
filestore-server/
├── main.go                # 入口、路由注册、优雅关闭
├── config/
│   └── config.go          # 环境变量配置
├── db/
│   ├── mysql/
│   │   └── conn.go        # MySQL 连接池
│   ├── file.go            # tbl_file CRUD（含按用户查询）
│   └── user.go            # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go         # 文件上传/下载/查询/删除 + 分片上传
│   ├── user.go            # 注册/登录 + bcrypt
│   ├── auth.go            # 认证中间件（Cookie session）
│   ├── ratelimit.go       # IP 限流中间件
│   └── cleanup.go         # 定时清理过期分片
├── meta/
│   └── filemeta.go        # 文件元数据结构 + MySQL 桥接
├── storage/
│   ├── storage.go         # Storage 接口定义
│   ├── minio.go           # MinIO 对象存储实现
│   └── local.go           # 本地文件存储实现
├── util/
│   ├── util.go            # SHA1、MD5、文件哈希、路径工具
│   ├── chunk.go           # 磁盘-based 分片追踪
│   └── resp.go            # JSON 响应辅助
├── scripts/
│   ├── start.sh           # Unix/macOS 启动脚本
│   └── start.bat          # Windows 启动脚本
├── migrations/            # SQL 迁移脚本
├── static/view/           # 前端 HTML 页面
├── uploads/               # 文件存储目录（本地 fallback）
├── chunks/                # 分片临时目录
├── .env.example           # 环境变量模板
├── Dockerfile             # 多阶段构建
├── docker-compose.yml     # Docker Compose 编排
└── AGENTS.md              # 多智能体开发协作文档
```

## 测试

```bash
go test ./...           # 运行全部测试
go test ./util/...      # 工具包测试
go test ./handler/...   # Handler 测试
```

测试覆盖：
- `util/` — SHA1、MD5、文件操作、路径工具、响应辅助
- `handler/` — HTTP 响应、状态码、JSON 格式、认证中间件、限流、边界情况（缺少参数、无效输入、资源不存在）

## 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | Gin | 高性能，社区活跃 |
| 存储 | MySQL + MinIO | 关系库 + 对象存储（MinIO + 本地 fallback） |
| 认证 | bcrypt + Cookie/Session | 密码哈希，Token 会话 |
| 日志 | log/slog | 结构化 JSON 输出 |
| 部署 | Docker Compose | 一键启动所有依赖 |

## 许可证

MIT