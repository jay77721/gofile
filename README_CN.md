<div align="center">

# gofile

**懂语义的轻量自建网盘** · Go + Gin + GORM + MySQL + MinIO + Redis + Typesense

[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go)](https://go.dev)
[![Gin](https://img.shields.io/badge/Gin-1.12-00ADD8?style=flat-square&logo=go)](https://gin-gonic.com)
[![GORM](https://img.shields.io/badge/GORM-1.31-00ADD8?style=flat-square&logo=go)](https://gorm.io)
[![MySQL](https://img.shields.io/badge/MySQL-8.0-4479A1?style=flat-square&logo=mysql&logoColor=white)](https://www.mysql.com)
[![MinIO](https://img.shields.io/badge/MinIO-S3-C72E49?style=flat-square&logo=minio)](https://min.io)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis)](https://redis.io)
[![Typesense](https://img.shields.io/badge/Typesense-Hybrid%20Search-00D4AA?style=flat-square)](https://typesense.org)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](#-许可证)

> 上传、秒传、分片断点续传、预签名直传直下 ——
> 以及最重要的:**用自然语言找到你的文件**。
>
> 「缓存优化资料」→ 返回 `Redis实战.pdf`、`MySQL高性能优化.md`、`Go后端架构笔记.docx`

[English](README.md) · [功能特性](#-功能特性) · [快速开始](#-快速开始) · [AI 语义检索](#-ai-语义检索) · [API 参考](#-api-参考) · [可观测性](#-可观测性)

</div>

---

## ✨ 功能特性

### 📁 文件能力

| 能力 | 说明 |
|------|------|
| 上传 / 秒传去重 | SHA1 哈希 + Redis 缓存加速,同一文件全局只存一份 |
| 分片上传 | 大文件切片、断点续传、幂等重试、服务端自动合并(分布式锁防竞态) |
| 下载 | HTTP Range 断点续传(`206 Partial Content`)、RFC 5987 文件名编码 |
| 在线预览 | 图片 / PDF / 视频 / 音频 / 文本 / 代码,按扩展名 + 文件头双重探测 MIME |
| 预签名直传直下 | S3 预签名 URL,客户端与 MinIO 直接通信,应用服务器零字节拷贝 |
| 软删除 + GC | 软删除不影响存储层;创建超过 7 天且无任何用户引用的文件,由后台 GC(24h 扫描)从存储层移除 |

### 🤖 AI 能力(可选,`AI_ENABLED=true` 启用)

| 能力 | 说明 |
|------|------|
| 自动摘要 | 上传后异步调 LLM,生成中文摘要(≤100 字) |
| 自动标签 | 按内容生成 3–5 个中文标签,支持类型过滤 |
| 语义搜索 | 自然语言查询 → 时间/类型解析 + 全文 + 向量混合检索(RRF 融合) |
| 相似推荐 | 以文搜文:给定文件,返回语义相近的文件 |
| 重复检测 | 近似重复文件识别(相似度阈值可调) |

### 🛠️ 工程能力

| 能力 | 说明 |
|------|------|
| 用户认证 | bcrypt 密码哈希 + HttpOnly Cookie Session(token 存 MySQL,24h 过期) |
| 全局限流 | `/user/signup`、`/user/signin` IP 限流(5 req/s):Redis 可用时用 Lua 固定窗口多实例共享,否则回退内存令牌桶 |
| 分布式锁 | Redis SETNX + Lua CAS,保护并发分片合并 |
| 降级策略 | MinIO 不可用 → 本地磁盘;Redis 不可用 → 内存实现;Typesense 不可用 → MySQL LIKE |
| 可观测性 | Prometheus 指标 + request_id 日志串联 + Grafana 大盘 |
| 安全 | 40 位 hex hash 校验防路径穿越、chunk 按用户隔离、Cookie `Secure` 可配置 |

---

## 🏗️ 系统架构

```
                     ┌──────────────────────────────────────────────┐
                     │                Gin HTTP Server               │
                     │  RequestID → Metrics → Recovery 中间件链      │
                     │                                              │
                     │  ┌──────────┐   ┌──────────────────────┐     │
                     │  │  Handler │──▶│      Service         │     │
                     │  │ auth/rate│   │ file / user / auth / │     │
                     │  │  limit   │   │ ai                   │     │
                     │  └──────────┘   └──────┬───────────────┘     │
                     └────────────────────────┼──────────────────────┘
                                              │
              ┌───────────────────────────────┼───────────────────────────────┐
              │                               │                               │
      ┌───────▼────────┐            ┌─────────▼─────────┐           ┌─────────▼─────────┐
      │  Repository     │            │      Storage      │           │   AI Pipeline     │
      │  GORM + MySQL   │            │  MinIO ⇄ Local    │           │  (异步,不阻塞上传) │
      │  tbl_file       │            │  (预签名/ Range)  │           │                   │
      │  tbl_user_file  │            └───────────────────┘           │  Enqueue          │
      │  tbl_user_token │                                            │   └─▶ extract     │
      │  tbl_ai_task    │                                            │   └─▶ analyze(LLM) │
      └─────────────────┘                                            │   └─▶ embed        │
              │                                                      │   └─▶ upsert       │
              │                                                      └────────┬─────────┘
              │                                                               │
      ┌───────▼─────────┐                    ┌───────────────────────────────▼──────────┐
      │     Redis       │                    │          Typesense (Hybrid)              │
      │ 秒传缓存/锁/限流 │                    │  全文 (filename+summary) + 向量 KNN      │
      └─────────────────┘                    │  RRF 融合 · 按 username 隔离索引          │
                                             └──────────────────────────────────────────┘
```

**分层与依赖方向**:`handler → service → repository + storage`,单向依赖,`main.go` 负责依赖注入组装。

### 数据模型:全局去重 × 用户隔离

- `tbl_file` — 全局文件注册表,按 `file_sha1` 唯一,文件内容只存一份
- `tbl_user_file` — 用户拥有关系,`UNIQUE(user_name, file_sha1)`,每个用户一行
- 秒传 = 命中 `tbl_file` 后仅为当前用户插入一条关联记录 —— 用户 B 秒传用户 A 的文件后,B 也能正常查询/下载

---

## 🚀 快速开始

### 方式一:Docker Compose(推荐)

```bash
git clone git@github.com:jay77721/gofile.git
cd gofile
docker compose up -d
```

| 服务 | 地址 | 说明 |
|------|------|------|
| 🌐 应用 | http://localhost:8080 | 主服务 |
| 🗄️ MinIO 控制台 | http://localhost:9001 | `minioadmin` / `minioadmin` |
| 🔍 Typesense | http://localhost:8108 | 检索引擎 |
| 📈 Prometheus | http://localhost:9090 | 指标抓取 |
| 📊 Grafana | http://localhost:3000 | `admin` / `admin`,预置 gofile Overview 大盘 |

> **启用 AI**:docker compose 默认不开启 AI,需在 `docker-compose.yml` 的 `app.environment` 中加入
> `AI_ENABLED=true`(以及 `AI_PROVIDER`、`AI_API_KEY`,见[配置](#-配置))后重新 `docker compose up -d`。

### 方式二:手动部署

```bash
# 1. 构建前端(Vue 3 + Vite,产物输出 web/dist)
cd web && npm install && npm run build && cd ..

# 2. 安装后端依赖
go mod tidy

# 3. 配置环境变量
cp .env.example .env    # 按需编辑

# 4. 启动(启动时 GORM AutoMigrate 自动建表)
go run main.go

# 或使用脚本
./start.sh              # 使用 .env 启动
./start.sh --migrate    # 先执行 schema.sql 再启动
./start.sh --build      # 构建二进制后运行
```

Windows 使用 `start.bat`,参数相同。

---

## 🤖 AI 语义检索

### 处理流水线(异步,不阻塞上传)

```
上传完成
   │
   ▼
Enqueue ──▶ worker pool ──▶ 提取文本(docx/pdf/pptx/txt/zip 清单…)
   │                          │
   │                          ▼
   │                    LLM 分析(摘要 + 标签)
   │                          │
   │                          ▼
   │                    Embedding(向量化)
   │                          │
   │                          ▼
   │                    MySQL 落库 + Typesense 建索引
   ▼
返回上传成功(毫秒级)

失败任务:retry_count < 3 自动补偿重试,7 天后清理
```

- 秒传命中(全局摘要已存在)时**跳过 LLM 调用**,直接建文档,零成本
- 任务状态机 `tbl_ai_task`:`pending → processing → done / failed`,支持补偿与过期清理

### 自然语言查询解析

`/file/ai/search?q=` 支持「时间 + 类型 + 语义」组合查询:

| 输入 | 解析结果 |
|------|----------|
| `最近3天上传的PDF` | 时间:近 3 天 · 类型:`tags:=[文档]` · 语义:「上传 PDF」 |
| `上周的Go面试资料` | 时间:上周 · 语义:「Go 面试资料」 |
| `今年图片` | 时间:今年 · 类型:`tags:=[图片]` |
| `数据库优化` | 语义:「数据库优化」(全文 + 向量混合检索) |

### 降级链

```
Typesense 可用 ──▶ Hybrid Search(全文 + 向量 + RRF 融合)
        │
        ▼ 不可用 / Embed 失败
MySQL LIKE 模糊搜索(filename + summary)——功能不中断
```

### 使用示例

```bash
# 语义搜索
curl -b cookies.txt "http://localhost:8080/file/ai/search?q=缓存优化资料"

# 相似文件推荐(以文搜文)
curl -b cookies.txt "http://localhost:8080/file/ai/similar?filehash=HASH&limit=5"

# 近似重复检测(相似度 ≥ 0.9)
curl -b cookies.txt "http://localhost:8080/file/ai/duplicates?filehash=HASH&threshold=0.9"
```

> 本地零成本体验:设置 `AI_PROVIDER=mock`,无需任何 API Key 即可跑通全链路
> (mock provider 生成确定性摘要/标签/向量,便于开发和测试)。

---

## ⚙️ 配置

全部通过环境变量配置,支持 `.env` 文件(见 `.env.example`)。

### 核心

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SERVER_ADDR` | `:8080` | HTTP 监听地址 |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?…` | MySQL 连接串,启动时 AutoMigrate |
| `COOKIE_SECURE` | `false` | 生产环境设为 `true`,Cookie 仅 HTTPS 传输 |

### 存储

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `MINIO_ENDPOINT` | `minio:9000` | 为空或连接失败 → 自动回退本地存储 |
| `MINIO_ACCESS_KEY` | `minioadmin` | — |
| `MINIO_SECRET_KEY` | `minioadmin` | — |
| `MINIO_BUCKET` | `filestore` | 存储桶 |
| `MINIO_USE_SSL` | `false` | — |
| `UPLOAD_DIR` | `./uploads` | 本地存储目录(回退用) |
| `CHUNK_DIR` | `./chunks` | 分片临时目录 |

### 缓存与限流(可选)

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `REDIS_ADDR` | `localhost:6379` | 为空 → 秒传缓存/分布式锁/限流自动降级 |
| `REDIS_PASSWORD` | `` | — |
| `REDIS_DB` | `0` | 数据库编号 |

### AI(可选,默认关闭)

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `AI_ENABLED` | `false` | AI 功能总开关 |
| `AI_PROVIDER` | `mock` | `mock`(免费跑通) / `openai` / `anthropic` |
| `AI_API_KEY` | `` | LLM API Key(mock 下忽略) |
| `AI_MODEL` | 供应商默认 | 模型名覆盖(如 `gpt-4o-mini`) |
| `AI_EMBED_DIM` | `128` | 向量维度 |
| `AI_WORKERS` | `4` | 异步分析 worker 数 |
| `TYPESENSE_URL` | `http://localhost:8108` | 检索引擎地址 |
| `TYPESENSE_API_KEY` | `xyz` | Typesense API Key |

---

## 📡 API 参考

统一响应格式:`{"code": 0|错误码, "msg": "...", "data": ...}`,`code=0` 表示成功;HTTP 状态码与业务错误码解耦。
所有 `/file/*` 与 `/user/info` 需要登录 Cookie(由 `/user/signin` 写入)。

### 错误码

| 错误码 | 含义 |
|--------|------|
| `1001` | 参数缺失、格式错误或不支持的操作 |
| `1002` | 未登录或登录状态无效 |
| `1003` | 已登录但无权操作该资源 |
| `1004` | 文件/资源不存在 |
| `1005` | 用户名已存在 |
| `1006` | 用户名或密码错误 |
| `1007` | 上传失败 |
| `1008` | 分片合并失败 |
| `1009` | 存储层错误(如预签名仅支持 MinIO) |
| `1010` | 请求过于频繁(限流) |
| `1011` | AI 检索失败 |
| `1099` | 服务内部错误(兜底) |

### 文件操作(需认证)

| 方法 | 路由 | 参数 | 说明 |
|------|------|------|------|
| `POST` | `/file/upload` | `file`(multipart),可选 `filehash`(触发秒传) | 上传,返回 `data.filehash` |
| `GET` | `/file/meta` | `filehash` | 文件元信息(含 AI 摘要/标签) |
| `GET` | `/file/query` | 可选 `page`、`size`(≤100) | 文件列表;带分页参数时返回 `{list, total, page, size}` |
| `GET` | `/file/download` | `filehash` | 下载,支持 `Range` 头(206) |
| `GET` | `/file/preview` | `filehash` | 在线预览(inline) |
| `POST` | `/file/update` | `op=0`、`filehash`、`filename` | 重命名 |
| `POST` | `/file/delete` | `filehash` | 软删除(进回收站) |
| `GET` | `/file/trash` | 可选 `page`、`size` | 回收站列表 |
| `POST` | `/file/restore` | `filehash` | 从回收站恢复 |
| `POST` | `/file/purge` | `filehash` | 彻底删除(不可恢复;无引用时同步清理存储层与索引) |

### 分片上传(需认证)

| 方法 | 路由 | 参数 | 说明 |
|------|------|------|------|
| `POST` | `/file/upload/chunk` | `filehash`、`index`、`file` | 上传单个分片,幂等,按用户隔离 |
| `GET` | `/file/upload/status` | `filehash` | 已上传分片索引(断点续传) |
| `POST` | `/file/upload/merge` | `filehash`、`filename`、`chunks` | 合并分片(分布式锁 + UUID 临时文件) |

### 预签名直传直下(需认证,仅 MinIO)

| 方法 | 路由 | 参数 | 说明 |
|------|------|------|------|
| `POST` | `/file/presigned/upload` | `filehash`、`filename` | 签发 PUT URL,客户端直传 MinIO |
| `POST` | `/file/presigned/upload/confirm` | `filehash`、`filename` | 直传完成后确认,写入元数据 |
| `GET` | `/file/presigned/download` | `filehash` | 签发 GET URL,客户端直下 |

### 文件分享

| 方法 | 路由 | 参数 | 认证 | 说明 |
|------|------|------|:----:|------|
| `POST` | `/file/share` | `filehash`、`days`(1-30,默认 7)、`password`(可选提取码) | ✓ | 创建分享,返回 `share_token` 与 `url` |
| `GET` | `/file/share/list` | — | ✓ | 我的分享列表 |
| `POST` | `/file/share/revoke` | `share_token` | ✓ | 撤销分享 |
| `GET` | `/share/:token` | `?pwd=`(可选) | × | 免登录下载(支持 Range,过期/撤销后 404,提取码错误 403) |

> 提示:提取码建议使用 8 位以上混合字符;公开下载路由有 IP 限流(10 req/s)防暴力破解,多用户共享出口 IP 时视频拖动可能触发限流。

### AI 检索(需认证,需启用 AI)

| 方法 | 路由 | 参数 | 说明 |
|------|------|------|------|
| `GET` | `/file/ai/search` | `q`、可选 `page`/`size` | 自然语言语义检索 |
| `GET` | `/file/ai/similar` | `filehash`、可选 `limit`(≤20,默认 5) | 相似文件推荐 |
| `GET` | `/file/ai/duplicates` | `filehash`、可选 `threshold`(默认 0.9) | 近似重复检测 |

### 用户与系统

| 方法 | 路由 | 参数 | 认证 | 限流 | 说明 |
|------|------|------|:----:|:----:|------|
| `POST` | `/user/signup` | `username`、`password` | × | ✓ | 注册 |
| `POST` | `/user/signin` | `username`、`password` | × | ✓ | 登录,token 仅经 HttpOnly Cookie 下发 |
| `GET` | `/user/info` | — | ✓ | × | 用户信息 |
| `GET` | `/healthz` | — | × | × | 健康检查 |
| `GET` | `/metrics` | — | × | × | Prometheus 指标 |
| `GET` | `/static/*` | — | × | × | 前端页面(Vue 3 SPA) |

### 快速体验

```bash
# 注册并登录(cookie 存本地)
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signup
curl -X POST -d "username=test&password=123456" http://localhost:8080/user/signin -c cookies.txt

# 上传
curl -X POST -F "file=@./test.txt" -b cookies.txt http://localhost:8080/file/upload

# 下载(带 Range)
curl -b cookies.txt -H "Range: bytes=0-1023" \
  "http://localhost:8080/file/download?filehash=HASH" -o partial.bin

# 分页查询
curl -b cookies.txt "http://localhost:8080/file/query?page=1&size=10"

# 预签名上传三步走
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload          # 1. 拿 URL
curl -X PUT -T ./test.txt "PRESIGNED_URL"               # 2. 直传 MinIO
curl -X POST -F "filehash=HASH" -F "filename=test.txt" -b cookies.txt \
  http://localhost:8080/file/presigned/upload/confirm   # 3. 确认

# 语义搜索
curl -b cookies.txt "http://localhost:8080/file/ai/search?q=缓存优化资料"
```

---

## 📊 可观测性

### Prometheus 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `http_requests_total` | Counter | `method, path, status` | HTTP 请求计数 |
| `http_request_duration_seconds` | Histogram | `method, path` | 请求耗时 |
| `file_upload_bytes_total` | Counter | — | 累计上传字节 |
| `ai_tasks_total` | Counter | `status` | AI 任务状态计数(pending/done/failed) |
| `ai_llm_duration_seconds` | Histogram | `operation` | LLM 调用耗时 |
| `ai_index_ops_total` | Counter | `operation, result` | 检索引擎操作计数 |

> `path` 标签使用路由模板(`c.FullPath()`)而非原始 URL,避免 filehash/query 造成标签基数爆炸。
> Docker Compose 已预置 Prometheus + Grafana,开箱即用。

### request_id 日志串联

每个请求生成 UUID(响应头 `X-Request-ID`)注入 context,自定义 `slog.Handler` 自动将其附加到该请求的所有日志——handler、service、访问日志可按请求串联排查:

```json
{"level":"INFO","msg":"access","method":"GET","path":"/file/upload","status":200,"request_id":"fd2d1cdf-..."}
{"level":"INFO","msg":"file uploaded","size":2048,"request_id":"fd2d1cdf-..."}
```

---

## 📁 项目结构

```
gofile/
├── main.go                 入口:依赖注入、路由注册、优雅关闭
├── schema.sql              建表脚本(参考;启动时 AutoMigrate)
├── config/                 环境变量配置(.env 支持)
├── model/                  GORM 领域模型(File/UserFile/User/Token/AITask)
├── repository/             GORM 数据访问层(接口 + 实现 + Mock)
├── service/                业务层(文件/用户/认证/AI 编排)
├── handler/                HTTP 层(文件/用户/鉴权/限流/后台任务/AI)
├── ai/                     AI 流水线:Provider 抽象 + Typesense 索引 + NLP 解析 + 文本抽取
│   ├── provider.go          Provider 接口(analyze/embed/dimension)
│   ├── factory.go           工厂:mock | openai | anthropic
│   ├── processor.go         worker pool + 任务状态机 + 失败补偿
│   ├── nlp.go               自然语言查询解析(时间/类型/停用词)
│   ├── extract.go           文本抽取(docx/pdf/pptx/zip…)
│   └── typesense.go         Hybrid Search 索引(全文 + 向量)
├── storage/                存储抽象(MinIO ⇄ Local,预签名/Range)
├── cache/                   Redis 封装(秒传缓存/分布式锁)
├── metrics/                 Prometheus 指标 + request_id 中间件
├── web/                     前端工程(Vue 3 + Vite,亮色简洁 SPA,构建产物 web/dist)
├── deploy/                  Prometheus 抓取配置 + Grafana 大盘预置
├── docker-compose.yml       MySQL + MinIO + Redis + Typesense + Prometheus + Grafana
└── start.sh / start.bat     启动脚本
```

---

## 🧰 技术栈

| 组件 | 选型 | 用途 |
|------|------|------|
| 语言 | Go 1.25 | — |
| Web 框架 | Gin | HTTP 路由与中间件 |
| ORM | GORM | AutoMigrate、预编译缓存、连接池(25 连接) |
| 数据库 | MySQL 8.0 | 元数据(全局文件 / 用户关系 / 会话 / AI 任务) |
| 对象存储 | MinIO(S3) | 文件内容 + 预签名 URL |
| 检索引擎 | Typesense | 全文 + 向量混合检索(RRF) |
| 缓存 | Redis 7(可选) | 秒传缓存、分布式锁、全局限流 |
| AI | OpenAI / Anthropic / Mock | 摘要、标签、Embedding |
| 指标 | Prometheus client_golang | 业务 + HTTP + AI 指标 |
| 监控 | Grafana | 预置大盘 |
| 前端 | Vue 3 + Vite(SPA,组件化) | 亮色简洁,侧边栏布局 |
| 部署 | Docker Compose | 一键编排 7 个服务 |

---

## 🧪 测试

```bash
go test ./...        # 全部测试
go vet ./...         # 静态检查
```

已覆盖:`handler/`(HTTP 响应、鉴权、限流、边界)、`metrics/`(request_id、指标中间件、全链路可观测性)、`ai/`(NLP 解析、mock provider、processor 状态机)、`util/`(哈希与文件工具)。

---

## 📄 许可证

MIT © [jay77721](https://github.com/jay77721)
