# gofile

<div align="center">

![gofile](https://img.shields.io/badge/gofile-v1.0-blue?style=flat-square&logo=go)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)
![Gin](https://img.shields.io/badge/Gin-1.12-0090D1?style=flat-square&logo=go)
![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql)
![MinIO](https://img.shields.io/badge/MinIO-Latest-C72E49?style=flat-square&logo=minio)
![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)

**轻量级自建网盘 | Lightweight Self-hosted File Storage**

[English](README.md) · [中文](#功能特性) · [快速开始](#快速开始) · [API 接口](#api-接口) · [项目结构](#项目结构)

</div>

---

## ✨ 功能特性

| | 功能 | 说明 |
|---|------|------|
| 📤 | **文件上传** | 普通上传 + 秒传去重（SHA1 hash） |
| 📥 | **文件下载** | 支持断点续传，Content-Disposition 安全编码 |
| ✂️ | **分片上传** | 大文件切片上传，断点续传，幂等重试，自动合并 |
| 🔐 | **用户认证** | bcrypt 密码哈希 + HttpOnly Cookie Session |
| 👤 | **文件归属** | 每个文件关联上传者，操作需校验所有权 |
| 🛡️ | **安全防护** | 路径穿越防护、IP 限流（5 req/s, burst 10）、RFC 5987 编码 |
| ☁️ | **存储后端** | MinIO (S3) 优先，失败自动回退本地磁盘 |
| 🧹 | **自动清理** | 过期分片定时清理（1h 间隔，24h 保留） |
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
                    │    MySQL    │◀────│   db/meta Layer  │
                    │  (metadata) │     │   (FileMeta)     │
                    └─────────────┘     └─────────┬────────┘
                                                  │
                              ┌───────────────────┼───────────────────┐
                              │                   │                   │
                    ┌─────────▼────────┐  ┌───────▼───────┐   ┌───────▼───────┐
                    │      MinIO       │  │  Local Disk   │   │   (future)    │
                    │   (S3 compat)    │  │  (fallback)   │   │               │
                    └──────────────────┘  └───────────────┘   └───────────────┘
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

### 方式二：手动部署

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

> 💡 服务会优先尝试连接 MinIO。若 `MINIO_ENDPOINT` 为空或 MinIO 初始化失败，自动回退到 `UPLOAD_DIR` 本地存储。

---

## 📡 API 接口

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

# 下载文件
curl -b cookies.txt \
  "http://localhost:8080/file/download?filehash=HASH" -o output.txt
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
- `handler/` — HTTP 响应, 状态码, JSON 格式, 认证中间件, 限流, 用户注册/登录验证, 边界情况（缺少参数, 无效输入, 不存在, panic 恢复）

---

## 📁 项目结构

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
│   ├── hash.go             # SHA1, MD5, 文件哈希, 路径工具
│   └── chunk.go            # 磁盘-based 分片追踪
├── static/                 # 前端 HTML 页面
├── start.sh                # Unix/macOS 启动脚本
├── start.bat               # Windows 启动脚本
├── .env.example            # 环境变量模板
├── Dockerfile              # 多阶段 Docker 构建
├── docker-compose.yml      # Docker Compose 编排
└── AGENTS.md               # AI 开发协作文档
```

---

## 🔧 技术栈

| 组件 | 选型 | 说明 |
|------|------|------|
| HTTP 框架 | Gin | 高性能，社区活跃 |
| 存储 | MySQL + MinIO | 关系库 + 对象存储（MinIO + 本地 fallback） |
| 认证 | bcrypt + Cookie/Session | 密码哈希，Token 会话 |
| 日志 | log/slog | 结构化 JSON 输出 |
| 部署 | Docker Compose | 一键启动所有依赖 |

---

## 📄 许可证

MIT

---

<div align="center">

Made with ❤️ by [jay77721](https://github.com/jay77721)

</div>
