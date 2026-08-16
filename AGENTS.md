# AGENTS.md

> AI 开发助手协作与架构规范文档 — 与 CLAUDE.md 保持完全同步

## 项目概览

gofile 是一个轻量级高性能自建网盘服务：Go + Gin + GORM/MySQL + MinIO (S3) + Redis + Typesense + Asynq + Vue 3。
支持文件上传/下载、S3 Multipart 预签名分片直传直合、秒传去重、HTTP Range 断点续传、VFS 树形虚拟文件系统（物化路径）、回收站、文件分享、Asynq 分布式任务调度，以及 AI 语义检索（摘要/智能标签/自然语言搜索/相似推荐/重复检测）。

## 快速启动

```bash
# 1) 前端构建（产物 web/dist，服务 /static 依赖它；未构建时首页 404）
cd web && npm ci && npm run build && cd ..

# 2) 数据库：只需创建空库 gofile，表结构由启动时自动迁移（migrations/）创建
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS gofile CHARACTER SET utf8mb4;"

# 3) 启动
cp .env.example .env
go run main.go        # 或 go build -o gofile . && ./gofile
```

## 目录结构

```
gofile/
├── main.go                入口、依赖组装、路由注册、优雅关闭、Swagger 注解
├── config/config.go       环境变量配置（Server/MySQL/MinIO/Redis/AI/Cookie/Asynq）
├── model/                 GORM 模型 + DTO：File/UserFile/User/Token/AITask/AIConfig/Share/Multipart
├── repository/            数据访问层：接口 + GORM 实现 + 内存 mock（file/user/token/share/ai_task/ai_config/multipart）
├── service/               业务层：file/user/auth/share/ai/ai_config
├── handler/
│   ├── handler.go         上传/秒传/下载(Range)/预览/回收站/分片/预签名/VFS/S3 Multipart
│   ├── user.go            注册/登录/登出/用户信息
│   ├── share.go           分享创建/列表/撤销/免登录下载
│   ├── ai.go              语义检索（搜索/相似/重复检测）
│   ├── ai_config.go       用户级 AI Provider 配置
│   ├── auth.go            AuthMiddleware（Cookie session 验证）
│   ├── ratelimit.go       IP 限流（Redis Lua 固定窗口 / 内存令牌桶回退）
│   ├── cleanup.go         后台任务：chunk 清理、分享清理、AI 补偿、AI 任务 TTL、软删除 GC
│   └── errcode.go         统一业务错误码 + respondError
├── ai/
│   ├── provider.go        Provider 接口（Analyze/Embed/Dimension）
│   ├── factory.go         mock | openai | anthropic 工厂
│   ├── openai.go / anthropic.go / mock.go
│   ├── extract.go / pdf.go   文本提取（text/pdf/office/压缩包，1MB 预算）
│   ├── nlp.go             对话式查询解析（时间/类型/停用词）
│   ├── typesense.go / indexer.go / indexer_mock.go   检索引擎
│   └── processor.go       异步任务编排（worker pool + 状态机 + 补偿；Asynq 优先，chan 降级）
├── task/                  M3 Asynq 分布式任务调度（hibiken/asynq）
│   ├── types.go           任务类型常量 + Payload 结构体
│   ├── client.go          生产者：Enqueue（实现 ai.TaskEnqueuer，幂等 TaskID）
│   ├── server.go          消费者：NewServer（ai 队列权重 6，slog 桥接）
│   └── processor.go       Asynq handler → ai.Processor.ProcessOne
├── storage/
│   ├── storage.go         Storage 接口（Put/Get/GetRange/FileSize/Exists/Delete/PresignPut/PresignGet/InitMultipart/PresignPartPut/CompleteMultipart/AbortMultipart）
│   ├── minio.go           MinIO 实现（自动建桶 + 预签名 + S3 Multipart）
│   └── local.go           本地磁盘实现（原子写；Multipart 返回 ErrPresignNotSupported）
├── cache/                 Redis 封装：hash 秒传缓存 + 分布式锁
├── metrics/               Prometheus 指标 + request_id + 访问日志中间件
├── util/                  SHA1/MD5、分片追踪、AES-GCM 加解密、SSRF URL 校验
├── db/mysql/              GORM 连接池 + 启动时自动跑 migrations/
├── migrations/            golang-migrate 版本化迁移 SQL（000001_init, 000002_multipart_and_vfs）
├── docs/                  Swagger 生成文档（docs.go/swagger.json/yaml）
├── web/                   Vue 3 前端工程（Vite + TS + Vitest + ESLint）
├── schema.sql             建表脚本（与 migrations/ 同步，初始化参考）
├── .github/workflows/ci.yml  CI：gofmt → vet → test -race → build + 前端构建 + Docker
├── BENCHMARKS.md          基准测试报告
├── UPGRADE_PLAN.md        升级方案与追踪清单
├── README.md / README_CN.md
└── CLAUDE.md              助手开发手册（与本文档保持同步）
```

## API 路由清单

### 文件模块（`/file` 组，全部鉴权）
| Method | Route | Description |
|--------|-------|-------------|
| POST | `/file/upload` | 上传（含秒传检测，100MB 上限） |
| GET | `/file/meta` | 文件元信息（含 AI 摘要/标签） |
| GET | `/file/query` | 文件列表（支持 `parent_id` 目录 + `page`/`size` 分页 + `breadcrumbs`） |
| GET | `/file/download` | 下载（支持 HTTP Range 206） |
| GET | `/file/preview` | 在线预览（MIME 探测） |
| POST | `/file/update` | 重命名（op=0） |
| POST | `/file/delete` | 软删除 |
| GET | `/file/trash` | 回收站列表（分页） |
| POST | `/file/restore` | 回收站恢复 |
| POST | `/file/purge` | 回收站彻底删除 |
| POST | `/file/share` | 创建分享（days 1-30 默认 7，password 可选） |
| GET | `/file/share/list` | 我的分享列表 |
| POST | `/file/share/revoke` | 撤销分享 |
| POST | `/file/upload/chunk` | 上传分片（幂等） |
| GET | `/file/upload/status` | 已上传分片索引 |
| POST | `/file/upload/merge` | 合并分片（分布式锁） |
| POST | `/file/presigned/upload` | 签发预签名上传 URL（15min） |
| POST | `/file/presigned/upload/confirm` | 确认预签名上传完成 |
| GET | `/file/presigned/download` | 签发预签名下载 URL（5min） |
| POST | `/file/upload/multipart/init` | 初始化 S3 Multipart（秒传命中直接返回，否则返回分片预签名 URL 列表） |
| POST | `/file/upload/multipart/complete` | 完成 S3 Multipart（前端汇报 ETag，存储层原子合并） |
| POST | `/file/upload/multipart/abort` | 取消 S3 Multipart 上传会话 |
| POST | `/file/folder/create` | 创建文件夹（`name` + 可选 `parent_id`） |
| POST | `/file/folder/rename` | 重命名文件/文件夹（`file_id` + `new_name`） |
| POST | `/file/folder/move` | 移动文件/文件夹（`file_id` + `target_parent_id`；防循环移动） |
| GET | `/file/ai/search` | 语义搜索（AI_ENABLED 时注册） |
| GET | `/file/ai/similar` | 相似文件推荐 |
| GET | `/file/ai/duplicates` | 近似重复检测 |

### 用户模块（`/user` 组）
| Method | Route | Auth | Rate Limit | Description |
|--------|-------|:----:|:----------:|-------------|
| POST | `/user/signup` | × | 5/s burst 10 | 注册 |
| POST | `/user/signin` | × | 5/s burst 10 | 登录，HttpOnly Cookie |
| POST | `/user/logout` | × | × | 登出（删 token + 清 Cookie） |
| GET | `/user/info` | ✓ | × | 用户信息 |

### 分享模块（公开）
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/share/:token?pwd=` | 免登录分享下载（限流 10/s burst 20，支持 Range） |

### AI 配置（`/ai/config` 组，鉴权，AI_ENABLED 时注册）
| Method | Route | Description |
|--------|-------|-------------|
| GET / POST / DELETE | `/ai/config` | 读取（key 掩码）/保存/清除用户级 Provider 配置 |
| POST | `/ai/config/test` | 连通性测试（对话 + embedding 双探测） |

### 系统与指标
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/healthz` | 健康检查 |
| GET | `/metrics` | Prometheus 指标 |
| GET | `/static/*` | 前端静态资源（`web/dist`） |
| GET | `/` | 重定向到 `/static/index.html` |

## 核心架构原则与约定

### 1. 架构分层与依赖注入
- `handler → service → repository + storage/cache/ai/task` 单向依赖，禁止反向引用。
- 所有组件通过构造器注入（`NewXxxService(repo, store, ...)`），禁止包级全局变量与隐式共享状态。
- 可选组件（Redis/AI/Typesense/Asynq）必须以 nil 安全方式注入，缺失或故障时优雅降级。

### 2. 统一错误码与响应规范
- 统一响应：`gin.H{"code": 0|错误码, "msg": "...", "data": ...}`，成功 `code = 0`。
- handler 统一使用 `respondError(c, httpStatus, code, msg)`（`handler/errcode.go`）。
- 错误码：1001 参数 / 1002 未登录 / 1003 越权 / 1004 不存在 / 1005 用户已存在 / 1006 凭据错误 / 1007 上传 / 1008 合并 / 1009 存储 / 1010 限流 / 1011 检索 / 1099 兜底。

### 3. 文件所有权模型（核心不变量）
- `tbl_file` 全局按 SHA1 去重；`tbl_user_file` 记录每个用户的拥有关系与 VFS 路径。
- 秒传：命中 `tbl_file` 后仅为当前用户插入 `tbl_user_file` 关联行并异步入队 AI 分析。
- **所有文件操作**必须先通过 `fileRepo.GetByHash(filehash, username)` 校验所有权，越权一律返回 1003。

### 4. VFS 虚拟文件系统
- 物化路径（Materialized Path）：`dir_path`（如 `/资料/Go/`）支持快速前缀查询任意子孙节点。
- 循环移动防护：移动文件夹时必须校验目标路径不能以自身路径为前缀。
- 重命名与移动时通过 SQL `CONCAT` + `SUBSTRING` 原子批量更新子孙节点前缀。

### 5. S3 Multipart 与存储层
- S3 Multipart 预签名直传：前端直接 PUT 到 MinIO，后端零本地磁盘 I/O、零中转带宽消耗。
- 存储抽象：MinIO 优先，本地磁盘兜底（原子写入：临时文件 + rename）；数据库写入失败必须回滚存储层（`store.Delete(ctx, key)`）。

### 6. Asynq 异步任务调度
- 双路任务队列：`ASYNQ_ENABLED=true` 时任务持久化至 Redis，支持多实例消费与 MaxRetry=3 指数退避；Redis 故障时自动降级进程内 chan。
- 任务幂等：TaskID 格式为 `username:filehash`；MySQL `tbl_ai_task` 状态机双保险。

---

## ⚡ 开发与提交规范 (CRITICAL RULES)

### 1. Git 提交规范（必须使用英文）
**所有 Git Commit 必须严格使用英文 Conventional Commits 格式**，严禁使用中文提交信息：
- 格式：`<type>(<scope>): <subject in english>`
- 常用 Type：
  - `feat`: 新功能（例如 `feat(task): implement asynq distributed worker pool`）
  - `fix`: 修复问题（例如 `fix(service): trigger ai enqueue on fast upload`）
  - `docs`: 文档更新（例如 `docs: sync AGENTS.md and CLAUDE.md specifications`）
  - `refactor`: 重构优化（例如 `refactor(ai): extract ProcessOne for task handler reuse`）
  - `test`: 补充测试（例如 `test(repository): add sqlite mock tests for vfs queries`）
  - `chore`/`build`/`ci`: 构建配置与依赖变更

### 2. 代码整洁度与极简原则（拒绝代码冗余）
- **简洁精炼**：遵循 Go 官方习惯（Go Idioms），保持代码扁平、直接，严禁过度设计与不必要的无意义封装。
- **杜绝冗余**：通用逻辑提取为单一真相源（Single Source of Truth），禁止在不同层或不同方法间复制粘贴重复逻辑。
- **单一职责**：每个接口与方法职责明确，参数精简，返回必要的 error。
- **严格错误处理**：I/O、DB、网络操作必须显式处理 error，使用 `slog.InfoContext/WarnContext/ErrorContext` 记录上下文并自动携带 `request_id`。
- **测试保证**：测试使用内存 SQLite / Mock，保证 `go test -race ./...` 持续全绿。

---

## 配置参考

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SERVER_ADDR` | `:8080` | HTTP 监听地址 |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?...` | MySQL DSN（必填，启动自动迁移） |
| `MINIO_ENDPOINT` | `127.0.0.1:9000` | MinIO 地址（留空则降级本地存储） |
| `REDIS_ADDR` | `localhost:6379` | Redis 地址（可选，提供秒传缓存/分布式锁/限流/Asynq） |
| `ASYNQ_ENABLED` | `false` | 是否启用 Asynq 分布式持久化队列（需 Redis，生产推荐开启） |
| `AI_ENABLED` | `false` | AI 功能总开关 |
| `AI_PROVIDER` | `mock` | `mock` \| `openai` \| `anthropic` |
| `TYPESENSE_URL` | `http://localhost:8108` | Typesense 检索引擎地址（不可用降级 MySQL LIKE） |
| `COOKIE_SECURE` | `false` | Cookie 仅 HTTPS 传输（生产置 true） |
| `ALLOW_PRIVATE_AI_URL` | `false` | 允许自定义 baseURL 指向内网（如本地 Ollama） |

## 已知问题与状态

- ~~🔴 `handler/handler.go` FastUpload 缺 enqueue~~ ✅ **已修复**（`50d7309`）：`FastUpload()` 末尾已补充 `enqueue` 调用
- 🟡 `web/dist` 未入库：本地/CI 需先 `cd web && npm run build`，否则 `/static` 404
- 🟡 Range 开放区间（`bytes=N-`）在未知文件大小时返回 416
- 🟡 分页为 `LIMIT/OFFSET`，大数据量深分页建议结合游标或主键过滤
