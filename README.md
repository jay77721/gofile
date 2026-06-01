# FileStore Server

> Gin + MinIO + MySQL + Redis

基于 Go 的轻量级网盘服务，支持文件上传/下载、分片上传与断点续传、用户认证、基于 hash 的秒传去重。

[English](README_EN.md)

## 功能特性

- **文件管理** — 上传、下载、查询、重命名、软删除，100MB 大小限制
- **用户认证** — 注册/登录，bcrypt 密码哈希，Cookie 会话（SameSite=Strict），24h Token 自动过期
- **分片上传** — 大文件分片、断点续传、自动合并、基于 hash 的秒传去重
- **存储后端** — 优先使用 MinIO（S3 兼容），MinIO 初始化失败时自动回退到本地文件存储
- **安全防护** — 路径穿越防护、IP 令牌桶限流、输入校验、安全随机 Token
- **可观测性** — 结构化 JSON 日志（log/slog）、健康检查端点、优雅关闭

## 快速开始

### Docker 部署（推荐）

```bash
git clone git@github.com:jay77721/FileStore-server.git
cd filestore-server
docker compose up -d
```

服务启动后访问：
- 应用服务：`http://localhost:8080`
- MinIO 控制台：`http://localhost:9001`（用户名/密码：minioadmin/minioadmin）

MySQL、Redis、MinIO 均由 Docker Compose 自动配置。

### 手动部署

**环境要求：** Go 1.25+、MySQL、Redis、MinIO

```bash
# 1. 安装依赖
go mod tidy

# 2. 设置环境变量
export MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/fileserver?charset=utf8mb4&parseTime=True&loc=Local"
export REDIS_ADDR="127.0.0.1:6379"
export MINIO_ENDPOINT="127.0.0.1:9000"
export MINIO_ACCESS_KEY="minioadmin"
export MINIO_SECRET_KEY="minioadmin"
export MINIO_BUCKET="filestore"

# 3. 初始化数据库
mysql -u root -p fileserver < migrations/000001_init_schema.up.sql
mysql -u root -p fileserver < migrations/000002_add_indexes.up.sql

# 4. 启动
go run main.go
```

## 配置

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL 连接字符串 |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis 地址 |
| `REDIS_PASS` | （空） | Redis 密码 |
| `REDIS_DB` | `0` | Redis DB 编号 |
| `SERVER_ADDR` | `:8080` | HTTP 监听地址 |
| `UPLOAD_DIR` | `./uploads` | 文件存储目录 |
| `CHUNK_DIR` | `./chunks` | 分片临时目录 |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO 服务地址 |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO Access Key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO Secret Key |
| `MINIO_BUCKET` | `filestore` | MinIO 存储桶名称 |
| `MINIO_USE_SSL` | `false` | 是否启用 SSL |

> 默认情况下，服务会优先尝试连接 MinIO。如果 `MINIO_ENDPOINT` 为空或 MinIO 初始化失败，将自动回退到本地存储目录（`UPLOAD_DIR`）。

## API 接口

### 文件操作（🔒 需认证）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/file/upload` | 上传文件（支持秒传） |
| `GET` | `/file/meta` | 获取文件元数据 |
| `GET` | `/file/query` | 查询所有文件 |
| `GET` | `/file/download` | 下载文件 |
| `POST` | `/file/update` | 重命名文件 |
| `POST` | `/file/delete` | 软删除文件 |

### 分片上传（🔒 需认证）

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/file/upload/chunk` | 上传单个分片 |
| `GET` | `/file/upload/status` | 查询已上传分片 |
| `POST` | `/file/upload/merge` | 合并分片为完整文件 |

### 用户操作

| 方法 | 路径 | 认证 | 限流 | 说明 |
|------|------|:----:|:----:|------|
| `POST` | `/user/signup` | ✗ | ✓ | 注册 |
| `POST` | `/user/signin` | ✗ | ✓ | 登录，返回 Token |
| `GET` | `/user/info` | ✓ | ✗ | 获取用户信息 |

### 系统

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/healthz` | 健康检查（Redis ping） |

## 使用示例

```bash
# 注册
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup

# 登录
curl -X POST -F "username=test&password=123456" http://localhost:8080/user/signin

# 上传文件
curl -X POST -F "file=@./test.txt" -b "username=test;token=YOUR_TOKEN" \
  http://localhost:8080/file/upload

# 下载文件
curl -b "username=test;token=YOUR_TOKEN" \
  "http://localhost:8080/file/download?filehash=HASH" -o output.txt
```

## 项目结构

```
filestore-server/
├── main.go              # 入口、路由注册、优雅关闭
├── config/config.go     # 环境变量配置
├── db/
│   ├── mysql/conn.go    # MySQL 连接池
│   ├── file.go          # tbl_file CRUD
│   └── user.go          # tbl_user / tbl_user_token CRUD
├── handler/
│   ├── handler.go       # 文件上传/下载/查询/删除 + 分片上传
│   ├── user.go          # 注册/登录 + bcrypt
│   ├── auth.go          # 认证中间件（Cookie session）
│   └── ratelimit.go     # IP 限流中间件
├── meta/filemeta.go     # 文件元数据结构 + MySQL 桥接
├── rd/redis.go          # Redis 连接 + hash 缓存
├── storage/             # MinIO 对象存储客户端
├── util/
│   ├── util.go          # SHA1、MD5、路径工具
│   ├── chunk.go         # Redis 分片追踪
│   └── resp.go          # JSON 响应辅助
├── migrations/          # SQL 迁移脚本
├── static/view/         # 前端 HTML 页面
├── uploads/             # 文件存储目录
├── Dockerfile           # 多阶段构建
└── docker-compose.yml   # Docker Compose 编排
```

## 测试

```bash
go test ./...           # 运行全部测试
go test ./util/...      # 工具包测试
go test ./handler/...   # Handler 测试
```

## 许可证

MIT
