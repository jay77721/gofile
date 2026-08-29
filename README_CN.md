<div align="center">

# gofile

**懂语义的工业级轻量自建网盘** · Go 1.25 + Gin + GORM + MySQL + MinIO (S3) + Redis + Asynq + Typesense + Vue 3 (TS)

[![CI](https://github.com/jay77721/gofile/actions/workflows/ci.yml/badge.svg)](https://github.com/jay77721/gofile/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Gin](https://img.shields.io/badge/Gin-1.12-00ADD8?style=flat-square&logo=go)](https://gin-gonic.com)
[![GORM](https://img.shields.io/badge/GORM-1.31-00ADD8?style=flat-square&logo=go)](https://gorm.io)
[![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql&logoColor=white)](https://www.mysql.com)
[![MinIO](https://img.shields.io/badge/MinIO-S3%20Multipart-C72E49?style=flat-square&logo=minio)](https://min.io)
[![Asynq](https://img.shields.io/badge/Asynq-Distributed%20Queue-FF6B6B?style=flat-square)](https://github.com/hibiken/asynq)
[![Typesense](https://img.shields.io/badge/Typesense-Hybrid%20Search-00D4AA?style=flat-square)](https://typesense.org)
[![Vue 3](https://img.shields.io/badge/Vue-3.5%20%2B%20TS-4FC08D?style=flat-square&logo=vue.js)](https://vuejs.org)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](#-许可证)

> 秒传去重、S3 分片直传原子合并、VFS 树形虚拟目录、HTTP Range 206 断点续传 ——
> 以及最重要的：**用自然语言智能检索并管理你的知识资产**。
>
> 「找一下上周关于分布式锁和数据库优化的资料」→ 毫秒级返回匹配文件、AI 核心摘要与标签。

[English](README.md) · [功能特性](#-功能特性) · [快速开始](#-快速开始) · [架构设计](#-架构设计) · [AI 语义检索](#-ai-语义检索) · [API 参考](#-api-参考) · [可观测性](#-可观测性)

</div>

---

## ✨ 功能特性

### 📁 核心存储与文件能力

| 能力 | 技术实现与说明 |
|---|---|
| **秒传去重 (Fast Upload)** | SHA1 哈希去重 + Redis 缓存加速，全局单一物理存储，多用户所有权秒级关联 |
| **S3 分片直传与原子合并 (M1)** | S3 预签名批量分片并发直传，MinIO 服务端零 I/O 拷贝原子合并，挂起分片 24h 自动 GC 清理 |
| **VFS 虚拟文件系统 (M2)** | 树形多级目录、物化路径（Materialized Path `dir_path`）、新建/重命名/移动、深度防循环嵌套移动（防自身移入子目录）、面包屑路径导航 |
| **流式下载 & 断点续传** | HTTP Range 206 字节区间请求响应，支持音视频按需拖动播放与大文件断点续传 |
| **智能在线预览** | 扩展名 + 文件头双重安全 MIME 嗅探，支持图片、PDF、文本、代码、音视频在线流畅预览 |
| **回收站与多级引用安全清理** | 软删除与活跃列表隔离；彻底清除（Purge）时进行全局引用计数检查，零引用时级联清理存储对象与索引 |

### 🤖 智能化 AI 语义流水线 (`AI_ENABLED=true`)

| 能力 | 技术实现与说明 |
|---|---|
| **异步任务编排 (M3)** | 基于 `hibiken/asynq` 工业级 Redis 异步任务队列，支持工作池并发消费、指数退避重试 (MaxRetry=3) 与死信队列，Redis 不可用时平滑回退内存 Channel |
| **自动摘要与智能标签** | 上传后异步触发 LLM 提炼中文核心摘要（≤100字）与多维度分类标签（文档/代码/多媒体等） |
| **自然语言多维检索** | 自然语言 Query 解析（时间解析如“最近3天” + 类型过滤 + 语义） + 全文向量混合检索（RRF 融合） |
| **用户级 Provider 配置** | 支持登录用户在网页端配置自定义 OpenAI 兼容 BaseURL 与 API Key（AES-GCM 加密存储，掩码回传） |
| **相似推荐与去重** | 基于 Typesense 向量 KNN 余弦相似度，提供以文搜文推荐与近似重复文件识别 |

### 🛠️ 工业级工程韧性与安全

* **架构分层解耦**：严格遵循 `Handler → Service → Repository/Storage/Task` 依赖注入单向架构，彻底杜绝单体巨石文件。
* **数据安全防护**：40 位 Hex 安全校验防路径穿越、文件扩展名黑名单防存储型 XSS、Cookie HttpOnly + Secure。
* **分布式限流与锁**：Redis + Lua 脚本实现多实例共享固定窗口限流（登录/注册 5 req/s），分布式锁保护并发分片合并。
* **全链路可观测性**：`X-Request-ID` 上下文日志串联 + Prometheus 业务与系统指标采集 + Grafana 预置大盘。
* **全栈 TypeScript**：前端 Vue 3 SFC 100% 采用 `<script setup lang="ts">`，前后端 API 契约与类型完全统一。

---

## 🏗️ 架构设计与拓扑

```
                             ┌──────────────────────────────────────────────────┐
                             │                 Gin HTTP Server                  │
                             │   RequestID ──▶ Prometheus ──▶ RateLimit 中间件  │
                             └────────────────────────┬─────────────────────────┘
                                                      │
                       ┌──────────────────────────────┴──────────────────────────────┐
                       ▼                                                             ▼
           ┌────────────────────────┐                                   ┌────────────────────────┐
           │      Handler 层        │                                   │      Service 领域层    │
           │ • handler.go (核心CRUD)│                                   │ • file_service.go      │
           │ • vfs.go (目录树路由)  │──────────────────────────────────▶│ • vfs_service.go (VFS) │
           │ • multipart.go (分片)  │                                   │ • multipart_service.go │
           │ • download.go (Range)  │                                   │ • trash_service.go     │
           └────────────────────────┘                                   └───────────┬────────────┘
                                                                                     │
                       ┌─────────────────────────────────────────────────────────────┼─────────────────────────────┐
                       ▼                                                             ▼                             ▼
           ┌────────────────────────┐                                   ┌────────────────────────┐    ┌────────────────────────┐
           │     Repository 层      │                                   │       Storage 层       │    │     Task 异步中枢 (M3) │
           │ GORM + MySQL 8.0       │                                   │ MinIO (S3) ⇄ 本地磁盘  │    │ Asynq Redis 队列       │
           │ • tbl_file (全局去重)  │                                   │ • S3 Multipart 直传    │    │ (Worker 并发 / 重试)   │
           │ • tbl_user_file (VFS)  │                                   │ • HTTP Range 206 流    │    │ (回退 In-Memory Chan)  │
           │ • tbl_multipart_upload │                                   └────────────────────────┘    └───────────┬────────────┘
           └────────────────────────┘                                                                              │
                       │                                                                                           ▼
                       ▼                                                                              ┌────────────────────────┐
           ┌────────────────────────┐                                                                 │      AI Pipeline       │
           │         Redis          │                                                                 │ • Text Extraction      │
           │ 秒传缓存 / 锁 / 限流   │                                                                 │ • LLM Summary & Tags   │
           └────────────────────────┘                                                                 │ • Typesense 向量索引   │
                                                                                                      └────────────────────────┘
```

---

## 🚀 快速开始

### 方式一：Docker Compose 一键启动（推荐）

```bash
git clone https://github.com/jay77721/gofile.git
cd gofile
docker compose -f docker/docker-compose.yml up -d
```

启动完成后各服务入口：
* 🌐 **网盘前端页面**：`http://localhost:8080`
* 🗄️ **MinIO 控制台**：`http://localhost:9001`（默认账号密码：`minioadmin` / `minioadmin`）
* 🔍 **Typesense 检索引擎**：`http://localhost:8108`
* 📈 **Prometheus 指标大盘**：`http://localhost:9090`
* 📊 **Grafana 监控看板**：`http://localhost:3000`（默认账号密码：`admin` / `admin`，预置 Gofile 仪表盘）

---

### 方式二：本地开发部署

```bash
# 1. 前端构建 (Vue 3 + Vite + TypeScript)
cd web
npm install
npm run build
cd ..

# 2. 安装后端依赖并配置环境
go mod tidy
cp .env.example .env

# 3. 运行后端服务 (服务启动时自动执行 migrations/ 数据库迁移)
go run main.go
```

---

## 🧪 自动化测试与 CI/CD

项目配备完整的端到端自动化测试与 GitHub Actions 流水线，严格保障代码健壮性：

```bash
# 1. 运行后端全量测试与竞态检测
go test -count=1 -race ./...

# 2. 运行前端 TypeScript 检查与单元测试
cd web && npm run build && npm test && cd ..

# 3. 性能基准测试
go test ./util/ -bench . -benchmem -run '^$'
```

---

## 📡 API 参考速查

统一响应规范：`{"code": 0|错误码, "msg": "...", "data": ...}`（`code = 0` 表示成功）。

### 1. 文件与 VFS 目录操作 (`/file`)

| 方法 | 路由 | 参数 / 请求体 | 说明 |
|---|---|---|---|
| `POST` | `/file/upload` | `file`(multipart) | 标准文件上传 / 秒传去重 |
| `GET` | `/file/meta` | `?filehash=` | 查询文件元信息（含 AI 摘要与标签） |
| `GET` | `/file/query` | `?parent_id=0&page=1&size=20` | VFS 目录列表查询（附带面包屑导航数据） |
| `POST` | `/file/folder/create` | `{"name":"Docs","parent_id":0}` | 创建虚拟文件夹 |
| `POST` | `/file/folder/rename` | `{"file_id":1,"new_name":"NewDocs"}` | 重命名文件或文件夹（自动级联更新子物化路径） |
| `POST` | `/file/folder/move` | `{"file_id":1,"target_parent_id":2}` | 移动文件或文件夹（**含深度防循环嵌套移动拦截**） |
| `GET` | `/file/download` | `?filehash=` (支持 Header `Range: bytes=0-1024`) | 流式下载与 HTTP Range 206 断点续传 |
| `GET` | `/file/preview` | `?filehash=` | 在线预览（图片/PDF/视频拖动播放/代码） |
| `POST` | `/file/delete` | `filehash` (Form) | 软删除文件入回收站 |
| `GET` | `/file/trash` | `?page=1&size=20` | 回收站列表查询 |
| `POST` | `/file/restore` | `filehash` (Form) | 恢复回收站文件（重新触发 AI 索引重建） |
| `POST` | `/file/purge` | `filehash` (Form) | 彻底删除（零引用时物理清理存储与向量文档） |

### 2. S3 分片直传与合并 (`/file/upload/multipart`)

| 方法 | 路由 | 参数 / 请求体 | 说明 |
|---|---|---|---|
| `POST` | `/file/upload/multipart/init` | `{"filehash":"...","filename":"a.zip","filesize":104857600}` | 初始化分片直传，签发批量预签名 PUT URL（秒传直通） |
| `POST` | `/file/upload/multipart/complete` | `{"upload_id":"...","parts":[{"part_number":1,"etag":"..."}]}` | 通知 MinIO 服务端原子合并 |
| `POST` | `/file/upload/multipart/abort` | `{"upload_id":"..."}` | 取消分片上传并释放存储资源 |

### 3. AI 语义检索与配置 (`/file/ai` & `/ai/config`)

| 方法 | 路由 | 参数 / 请求体 | 说明 |
|---|---|---|---|
| `GET` | `/file/ai/search` | `?q=最近3天上传的微服务架构资料` | 自然语言多维混合检索（时间+类型+全文+向量 RRF） |
| `GET` | `/file/ai/similar` | `?filehash=...&limit=5` | 语义相近文件推荐（以文搜文） |
| `GET` | `/file/ai/duplicates` | `?filehash=...&threshold=0.9` | 近似重复文档识别 |
| `GET/POST/DELETE` | `/ai/config` | `{"base_url":"...","api_key":"...","model":"..."}` | 用户级独立 AI Provider 设置（加密存储与掩码回传） |
| `POST` | `/ai/config/test` | `{"base_url":"...","api_key":"..."}` | 在线探测 AI 端点连通性 |

---

## 📁 模块化项目结构

```
gofile/
├── main.go                       # 服务入口：依赖注入组装与优雅停机
├── docker/                      # Dockerfile、Compose 与监控部署配置
├── migrations/                   # golang-migrate 版本化 SQL 迁移脚本
├── config/                       # 环境变量加载与类型映射
├── model/                        # 领域模型 (File, UserFile, Multipart, User, Token, AI)
├── repository/                   # GORM 数据持久层与内存 Mock
├── service/                      # 业务领域服务层 (单一职责解耦)
│   ├── file_service.go            # 核心文件 CRUD 与流下载
│   ├── vfs_service.go             # 树形目录、物化路径与防循环移动
│   ├── multipart_service.go       # S3 Multipart 直传与传统分片合并
│   ├── trash_service.go           # 回收站生命周期与级联清理
│   ├── user_service.go            # 用户注册管理
│   ├── auth_service.go            # Token 鉴权服务
│   ├── share_service.go           # 文件免密/提取码分享
│   └── ai_service.go              # AI 语义搜索与推荐
├── handler/                      # HTTP 路由控制器层 (高内聚解耦)
│   ├── handler.go                 # 核心文件操作与健康检查
│   ├── vfs.go                     # VFS 目录树操作接口
│   ├── multipart.go               # S3 Multipart 分片直传接口
│   ├── download.go                # Range 206 字节解析、下载与在线预览
│   ├── user.go & auth.go          # 用户登录注册与鉴权中间件
│   ├── share.go                   # 分享接口
│   ├── ai.go & ai_config.go       # AI 搜索与 Provider 配置
│   ├── cleanup.go                 # 挂起分片与无主孤立文件后台清理 Worker
│   └── ratelimit.go & errcode.go  # 分布式限流与统一错误码
├── task/                         # Asynq 分布式任务调度中枢 (Client / Server / Processor)
├── ai/                           # AI 底座：LLM 提取 / NLP 查询解析 / Typesense 向量索引
├── storage/                      # 存储适配层 (MinIO S3 预签名/原子合并 ⇄ 本地磁盘)
├── cache/                        # Redis 分布式锁 / 限流 / 秒传缓存
├── metrics/                      # Prometheus 业务指标采集与 X-Request-ID 链路追踪
└── web/                          # 前端工程 (Vue 3 + Vite 6 + TypeScript 5 + Vitest)
    └── src/
        ├── components/           # 14 个严格 TypeScript Vue 组件 (<script setup lang="ts">)
        ├── api.ts                # 严格强类型前后端 API 契约层
        └── utils.ts & toast.ts   # 工具函数与全局提示
```

---

## 📄 许可证

MIT © [jay77721](https://github.com/jay77721)
