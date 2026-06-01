# 文件存储服务器 (FileStore Server)

基于 Go 语言开发的轻量级文件存储服务器，支持文件上传、下载、用户管理和分片上传等功能。

## 🚀 功能特性

- 📁 **文件管理**
  - 文件上传（支持元数据，100MB 大小限制）
  - 文件下载
  - 文件元数据更新
  - 文件删除（软删除）
  - 文件信息查询

- 🔐 **用户认证**
  - 用户注册
  - 用户登录
  - 用户信息获取
  - 基于 Cookie 的会话认证
  - bcrypt 密码哈希

- ⚡ **分片上传**
  - 支持大文件分片上传
  - 分片上传状态检查
  - 自动合并分片
  - 秒传（基于 hash 去重）

- 🗄️ **存储后端**
  - MySQL 数据库存储元数据
  - Redis 缓存和分片追踪
  - 本地文件系统存储

- 🛡️ **安全特性**
  - bcrypt 密码哈希
  - 安全随机 token 生成（crypto/rand）
  - 路径穿越防护（filepath.Base）
  - 所有文件接口需要登录认证
  - 输入验证和大小限制

## 🏗️ 项目架构

```
filestore-server/
├── main.go              # 程序入口、HTTP 路由、优雅关闭
├── config/
│   └── config.go        # 基于环境变量的配置管理
├── db/                  # 数据库操作
│   ├── mysql/conn.go    # MySQL 连接池
│   ├── file.go          # 文件相关数据库操作
│   └── user.go          # 用户相关数据库操作
├── handler/             # HTTP 请求处理器
│   ├── auth.go          # 认证中间件
│   ├── handler.go       # 文件上传下载处理器
│   └── user.go          # 用户管理处理器
├── meta/                # 文件元数据管理
│   └── filemeta.go      # 文件元数据结构和数据库桥接
├── rd/                  # Redis 操作
│   └── redis.go         # Redis 连接和缓存操作
├── util/                # 工具函数
│   ├── util.go          # 哈希工具（SHA1、MD5）
│   ├── chunk.go         # 分片上传工具
│   └── resp.go          # JSON 响应辅助函数
├── static/              # 静态文件（前端资源）
├── uploads/             # 上传文件存储
└── go.mod               # Go 模块定义
```

## 🛠️ 技术栈

- **编程语言**: Go 1.25.0
- **数据库**: MySQL
- **缓存**: Redis
- **Web 框架**: net/http（标准库）
- **认证方式**: 基于 Cookie 的会话 + bcrypt

## 📋 API 接口

### 文件操作（🔒 需要登录认证）

| 方法 | 接口 | 描述 |
|------|------|------|
| POST | `/file/upload` | 上传文件 |
| GET | `/file/meta` | 获取文件元数据 |
| GET | `/file/query` | 查询所有文件 |
| GET | `/file/download` | 下载文件 |
| POST | `/file/update` | 更新文件元数据 |
| POST | `/file/delete` | 删除文件（软删除） |

### 分片上传（🔒 需要登录认证）

| 方法 | 接口 | 描述 |
|------|------|------|
| POST | `/file/upload/chunk` | 上传文件分片 |
| GET | `/file/upload/status` | 检查分片上传状态 |
| POST | `/file/upload/merge` | 合并上传的分片 |

### 用户操作

| 方法 | 接口 | 认证 | 描述 |
|------|------|------|------|
| POST | `/user/signup` | 否 | 用户注册 |
| POST | `/user/signin` | 否 | 用户登录 |
| GET | `/user/info` | 是 | 获取用户信息 |

### 系统

| 方法 | 接口 | 描述 |
|------|------|------|
| GET | `/healthz` | 健康检查（MySQL + Redis） |

## 🚦 快速开始

### Docker 部署（推荐）

```bash
# 克隆并使用 Docker Compose 启动
git clone <仓库地址>
cd filestore-server
docker compose up -d
```

服务将在 `http://localhost:8080` 启动，MySQL 和 Redis 会自动配置。

### 手动部署

#### 环境要求

- Go 1.25.0 或更高版本
- MySQL 数据库
- Redis 服务器

#### 安装部署

1. **克隆项目**
   ```bash
   git clone <仓库地址>
   cd filestore-server
   ```

2. **安装依赖**
   ```bash
   go mod tidy
   ```

3. **设置环境变量**
   ```bash
   # MySQL 连接字符串（必填）
   export MYSQL_DSN="user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local"

   # Redis 配置（可选，以下为默认值）
   export REDIS_ADDR="127.0.0.1:6379"
   export REDIS_PASS=""
   export REDIS_DB=0

   # 服务器配置（可选，以下为默认值）
   export SERVER_ADDR=":8080"
   export UPLOAD_DIR="./uploads"
   export CHUNK_DIR="./chunks"
   ```

4. **创建数据库表**
   ```sql
   CREATE TABLE tbl_file (
     file_sha1 char(40) NOT NULL PRIMARY KEY,
     file_name varchar(256) NOT NULL DEFAULT '',
     file_size bigint(20) DEFAULT 0,
     file_addr varchar(512) DEFAULT '',
     create_at datetime DEFAULT CURRENT_TIMESTAMP,
     status tinyint(4) NOT NULL DEFAULT 0 COMMENT '0-正常, 1-已删除, 2-禁止'
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

   CREATE TABLE tbl_user (
     user_name varchar(64) NOT NULL PRIMARY KEY,
     user_pwd varchar(60) NOT NULL DEFAULT '' COMMENT 'bcrypt 哈希值',
     signup_at datetime DEFAULT CURRENT_TIMESTAMP,
     status tinyint(4) NOT NULL DEFAULT 0
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

   CREATE TABLE tbl_user_token (
     user_name varchar(64) NOT NULL PRIMARY KEY,
     user_token char(64) NOT NULL DEFAULT '',
     update_at datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
     expired_at datetime
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
   ```

5. **启动服务器**
   ```bash
   go run main.go
   ```

6. **访问服务**
   - 服务器将在 `http://localhost:8080` 启动
   - 静态文件通过 `/static/` 访问
   - 健康检查：`http://localhost:8080/healthz`

## 📝 使用示例

### 上传文件
```bash
curl -X POST -F "file=@/path/to/your/file.txt" \
  -b "username=testuser;token=your_token" \
  http://localhost:8080/file/upload
```

### 下载文件
```bash
curl -X GET "http://localhost:8080/file/download?filehash=abc123" \
  -b "username=testuser;token=your_token" \
  --output file.txt
```

### 用户注册
```bash
curl -X POST -d "username=testuser&password=password123" \
  http://localhost:8080/user/signup
```

### 用户登录
```bash
curl -X POST -F "username=testuser&password=password123" \
  http://localhost:8080/user/signin
```

## ⚙️ 配置说明

所有配置通过环境变量设置，均有合理默认值：

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/fileserver?...` | MySQL 连接字符串 |
| `REDIS_ADDR` | `127.0.0.1:6379` | Redis 服务器地址 |
| `REDIS_PASS` | （空） | Redis 密码 |
| `REDIS_DB` | `0` | Redis 数据库编号 |
| `SERVER_ADDR` | `:8080` | HTTP 服务器监听地址 |
| `UPLOAD_DIR` | `./uploads` | 文件上传目录 |
| `CHUNK_DIR` | `./chunks` | 分片上传目录 |

## 🔒 安全特性

- **密码哈希**: 使用 bcrypt（默认 cost）
- **Token 生成**: 32 字节随机 token，通过 crypto/rand 生成
- **路径穿越防护**: 所有文件名通过 filepath.Base 过滤
- **接口认证**: 所有文件操作需要有效的会话 Cookie
- **输入验证**: 文件大小限制、参数校验
- **软删除**: 文件标记为已删除，不物理移除

## 🚧 开发状态

该项目正在积极开发中，功能可能会发生变化，API 接口可能会修改。

## 🤝 贡献指南

1. Fork 项目仓库
2. 创建功能分支
3. 进行代码修改
4. 如果适用，添加测试
5. 提交 Pull Request

## 📄 许可证

本项目用于教育和学习目的。

## 🆘 技术支持

如有问题或建议，请在仓库中提交 Issue。

---

**注意**: 本项目仅供学习和研究使用，请勿用于商业用途。
