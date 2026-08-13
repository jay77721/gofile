# CLAUDE.md

## Project Overview

gofile 是一个轻量级自建网盘服务：Go + Gin + GORM/MySQL + MinIO + Redis + Typesense + Vue 3。
支持文件上传/下载、分片上传断点续传、秒传去重、预签名直传直下、回收站、文件分享，以及 AI 语义检索（摘要/标签/自然语言搜索）。

## Tech Stack

- **Language:** Go 1.25.0
- **HTTP:** Gin 1.12
- **Database:** GORM 1.31 + MySQL（`golang-migrate` 版本化迁移；连接池 25 打开 / 10 空闲 / 5min 生命周期）
- **Storage:** `storage.Storage` 接口 — MinIO (S3) 优先，失败自动 fallback 本地磁盘（原子写）
- **Cache:** go-redis v9（可选）— 秒传缓存 / 分布式锁 / 全局限流；不可用时自动降级内存实现
- **Search:** Typesense（可选）— 全文 + 向量混合检索（RRF 融合）；不可用降级 MySQL LIKE
- **AI:** Provider 抽象（mock / openai / anthropic）+ 异步 worker pool；支持用户级自定义 OpenAI 协议 baseURL + API key
- **Auth:** bcrypt + HttpOnly Cookie Session（token 存 MySQL `tbl_user_token`，24h 过期）
- **Observability:** Prometheus 指标 + `log/slog` 结构化日志（request_id 串联）
- **Frontend:** Vue 3 + Vite + TypeScript（`web/`，构建产物 `web/dist`）
- **Docs:** Swagger 注解（`docs/` 生成物）、`BENCHMARKS.md` 基准报告

## Project Structure

```
gofile/
├── main.go                入口、依赖组装、路由注册、优雅关闭、Swagger 注解
├── config/config.go       环境变量配置（Server/MySQL/MinIO/Redis/AI/Cookie 安全）
├── model/                 GORM 模型 + DTO：File/UserFile/User/Token/AITask/AIConfig/Share
├── repository/            数据访问层：接口 + GORM 实现 + 内存 mock（file/user/token/share/ai_task/ai_config）
├── service/               业务层：file/user/auth/share/ai/ai_config
├── handler/
│   ├── handler.go         上传/秒传/下载(Range)/预览/回收站/分片/预签名
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
│   └── processor.go       异步任务编排（worker pool + 状态机 + 补偿）
├── storage/
│   ├── storage.go         Storage 接口（Put/Get/GetRange/FileSize/Exists/Delete/PresignPut/PresignGet）
│   ├── minio.go           MinIO 实现（自动建桶 + 预签名）
│   └── local.go           本地磁盘实现（原子写）
├── cache/                 Redis 封装：hash 秒传缓存 + 分布式锁
├── metrics/               Prometheus 指标 + request_id + 访问日志中间件
├── util/                  SHA1/MD5、分片追踪、AES-GCM 加解密、SSRF URL 校验
├── db/mysql/              GORM 连接池 + 启动时自动跑 migrations/
├── migrations/            golang-migrate 版本化迁移 SQL
├── docs/                  Swagger 生成文档（docs.go/swagger.json/yaml）
├── web/                   Vue 3 前端工程（Vite + TS + Vitest + ESLint）
├── schema.sql             建表脚本（与 migrations/ 同步，初始化参考）
├── .github/workflows/ci.yml  CI：gofmt → vet → test -race → build + 前端构建 + Docker
├── BENCHMARKS.md          基准测试报告
├── README.md / README_CN.md
└── AGENTS.md              AI 开发协作文档（与本文档保持同步）
```

## Build & Run

```bash
# 1) 前端构建（产物 web/dist，服务 /static 依赖它；未构建时首页 404）
cd web && npm ci && npm run build && cd ..

# 2) 数据库：只需创建空库 gofile，表结构由启动时自动迁移（migrations/）创建
mysql -u root -p -e "CREATE DATABASE IF NOT EXISTS gofile CHARACTER SET utf8mb4;"

# 3) 启动
cp .env.example .env   # 按需修改
go build -o gofile .
./gofile               # Windows: gofile.exe / start.bat

# Docker
docker compose up -d
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_ADDR` | `:8080` | HTTP listen address |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/gofile?...` | MySQL DSN（必填，启动自动迁移） |
| `UPLOAD_DIR` | `./uploads` | Local storage directory (fallback) |
| `CHUNK_DIR` | `./chunks` | Chunk temp directory |
| `MINIO_ENDPOINT` | `minio:9000` | MinIO endpoint (empty = skip MinIO) |
| `MINIO_ACCESS_KEY` / `MINIO_SECRET_KEY` | `minioadmin` | MinIO credentials |
| `MINIO_BUCKET` | `filestore` | MinIO bucket (auto-created) |
| `MINIO_USE_SSL` | `false` | Enable SSL for MinIO |
| `COOKIE_SECURE` | `false` | Cookie 仅 HTTPS 传输（生产置 true） |
| `REDIS_ADDR` / `REDIS_PASSWORD` / `REDIS_DB` | `localhost:6379` / 空 / 0 | Redis（不可用自动降级） |
| `AI_ENABLED` | `false` | AI 功能总开关 |
| `AI_PROVIDER` | `mock` | `mock` \| `openai` \| `anthropic` |
| `AI_API_KEY` | 空 | LLM API Key（mock 忽略） |
| `AI_MODEL` | 空 | LLM 模型名（空用 provider 默认） |
| `AI_EMBED_DIM` | `128` | 向量维度（Typesense collection 创建后不可改） |
| `AI_WORKERS` | `4` | 异步 worker 数量 |
| `TYPESENSE_URL` / `TYPESENSE_API_KEY` | `http://localhost:8108` / `xyz` | 检索引擎（不可用降级 LIKE） |
| `AI_CONFIG_SECRET` | 空 | 用户 API key 加密密钥（未配置从 DSN 派生） |
| `ALLOW_PRIVATE_AI_URL` | `false` | 允许自定义 baseURL 指向内网（本地 Ollama 场景） |

> 所有可选依赖（Redis/AI/Typesense）缺失时服务照常启动并降级。

## API Endpoints

### 文件（`/file` 组，全部需要鉴权）
| Method | Route | Description |
|--------|-------|-------------|
| POST | `/file/upload` | 上传（含秒传检测，100MB 上限） |
| GET | `/file/meta` | 文件元信息（含 AI 摘要/标签） |
| GET | `/file/query` | 文件列表（支持 `page`/`size` 分页） |
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
| GET | `/file/ai/search` | 语义搜索（AI_ENABLED 时注册） |
| GET | `/file/ai/similar` | 相似文件推荐 |
| GET | `/file/ai/duplicates` | 近似重复检测 |

### 用户
| Method | Route | Auth | Rate Limit | Description |
|--------|-------|:----:|:----------:|-------------|
| POST | `/user/signup` | × | 5/s burst 10 | 注册 |
| POST | `/user/signin` | × | 5/s burst 10 | 登录，HttpOnly Cookie |
| POST | `/user/logout` | × | × | 登出（删 token + 清 Cookie） |
| GET | `/user/info` | ✓ | × | 用户信息 |

### 分享（公开）
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/share/:token?pwd=` | 免登录分享下载（限流 10/s burst 20，支持 Range） |

### AI 配置（`/ai/config` 组，鉴权，AI_ENABLED 时注册）
| Method | Route | Description |
|--------|-------|-------------|
| GET / POST / DELETE | `/ai/config` | 读取（key 掩码）/保存/清除用户级 Provider 配置 |
| POST | `/ai/config/test` | 连通性测试（对话 + embedding 双探测） |

### 系统
| Method | Route | Description |
|--------|-------|-------------|
| GET | `/healthz` | 健康检查 |
| GET | `/metrics` | Prometheus 指标 |
| GET | `/static/*` | 前端静态资源（`web/dist`） |
| GET | `/` | 重定向到 `/static/index.html` |

## Architecture Notes

### 三层架构与依赖注入
`handler → service → repository + storage/cache/ai` 单向依赖，全部通过构造器注入（`NewXxxHandler(svc, cfg)`），`main.go` 负责组装。可选依赖（Redis/AI/Typesense）以 nil 安全方式注入，缺失时自动降级。

### 响应格式与统一错误码
- 统一响应：`gin.H{"code": 0|错误码, "msg": "...", "data": ...}`，成功 code=0
- handler 统一使用 `respondError(c, httpStatus, code, msg)`（`handler/errcode.go`）

| 码 | 常量 | 含义 |
|----|------|------|
| 0 | CodeOK | 成功 |
| 1001 | CodeInvalidParams | 参数缺失/格式错误 |
| 1002 | CodeUnauthorized | 未登录/登录失效 |
| 1003 | CodeForbidden | 越权 |
| 1004 | CodeNotFound | 不存在 |
| 1005 | CodeUserExists | 用户名已存在 |
| 1006 | CodeInvalidCreds | 用户名或密码错误 |
| 1007 | CodeUploadFailed | 上传失败 |
| 1008 | CodeMergeFailed | 分片合并失败 |
| 1009 | CodeStorageError | 存储层错误（如预签名仅支持 MinIO） |
| 1010 | CodeTooManyRequests | 限流 |
| 1011 | CodeSearchFailed | AI 检索失败 |
| 1099 | CodeInternalError | 兜底 |

### Auth Flow
1. signup：bcrypt 哈希 → `tbl_user`（用户名主键，重复返回 1005）
2. signin：bcrypt 验证 → `crypto/rand` 生成 64 位 hex token → `tbl_user_token`（24h 过期）
3. `Set-Cookie`：HttpOnly + SameSite=Lax + Secure（`COOKIE_SECURE`），1h 有效期
4. `AuthMiddleware`：Cookie username+token → `tokenRepo.Get` → 比对 + 过期校验，失败 1002
5. logout：删除服务端 token + 清除 Cookie（幂等）
6. Token **永不**出现在 JSON 响应体，仅通过 Cookie 传递

### 存储层
- `storage.Storage`：`Put / Get / GetRange / FileSize / Exists / Delete / PresignPut / PresignGet`
- MinIO 优先，失败 fallback 本地磁盘；本地 `Put` 为原子写（临时文件 + rename）
- 本地存储的 `PresignPut/PresignGet` 返回 `ErrPresignNotSupported`
- MySQL 只存元数据；文件内容在存储层，key = `file_sha1`

### 文件所有权（全局去重 × 用户隔离）
- `tbl_file` 全局去重（`file_sha1` 主键），`tbl_user_file` 每用户一行（`UNIQUE(user_name, file_sha1)`）
- 秒传 = 命中全局文件后仅为当前用户插入关联行 → 用户 B 秒传用户 A 的文件后 B 可查可下
- 下载/重命名/删除/分享等所有操作先经 `fileRepo.GetByHash(filehash, username)` 校验所有权，无权返回 1003

### 分片上传
- 目录结构：`<CHUNK_DIR>/<username>/<filehash>/<index>`（用户隔离 + 40 位 hex 校验防路径穿越）
- chunk 上传幂等（已存在直接返回）；`/upload/status` 查询进度；merge 校验分片总数
- merge 并发保护：Redis 分布式锁 `gofile:lock:merge:<hash>`（SETNX + Lua CAS，2min 自动过期）+ UUID 临时文件防冲突
- chunk 清理：超过 24h 未更新的分片目录，每小时扫描（`handler/cleanup.go`）

### 回收站与 GC
- 软删除 `status=2`（存储层不动）；`/trash` 分页列表；`/restore` 恢复（并重入队 AI 重建索引）；`/purge` 彻底删除
- purge：删除关联行后引用数为 0 时同步清理存储层 + `tbl_file` + 检索引擎
- 后台 GC：创建超 7 天且 0 活跃引用的文件，24h 扫描一次，从存储层移除并清理 Typesense 文档

### 文件分享
- `tbl_share`：64 位 hex 令牌（crypto/rand，防枚举）+ 可选 bcrypt 提取码 + 有效期 1-30 天（默认 7）
- 免登录下载 `GET /share/:token`（限流 10/s burst 20 防提取码爆破），支持 Range 断点
- 解析时校验：过期、提取码、文件仍存在（以分享者身份校验所有权）；密码哈希永不下发（`HasPassword` 仅标记）
- 每日清理过期分享

### 限流
- `/user/signup`、`/user/signin`：5 req/s burst 10（`c.ClientIP()`）
- Redis 可用：Lua 固定窗口（`gofile:ratelimit:<ip>`，多实例共享）；否则内存令牌桶（空闲 IP 5min 回收）

### AI 异步管线（`AI_ENABLED=true`）
- `ai.Provider` 接口：`Analyze / Embed / Dimension`；`factory.go` 按 `AI_PROVIDER` 选择 mock|openai|anthropic
- `ai.Processor`：worker pool（`AIWorkers` 默认 4，队列容量 100，满则丢弃不阻塞上传）+ 任务状态机（0 待处理 / 1 处理中 / 2 完成 / 3 失败）
- 管线：extract（文本/PDF/Office/压缩包，1MB 读取预算）→ analyze（LLM 摘要+标签）→ embed → `SaveAnalysis` → Typesense upsert
- 秒传命中复用全局 summary/tags，跳过 LLM 零成本建文档；失败任务 retry ≤3，补偿退避 1m→30m；任务 7 天 TTL 每日清理
- 任务幂等锚点：`tbl_ai_task` UNIQUE(`file_sha1`, `user_name`)

### AI 语义检索
- `/file/ai/search?q=`：`ai.ParseQuery` 解析时间短语/类型词/停用词 → Embed → `SearchHybrid`（全文 + 向量 KNN，RRF 融合，按 username 过滤，文档 id = `username:filehash`）
- `/file/ai/similar`、`/file/ai/duplicates`（相似度阈值默认 0.9）
- Typesense 不可用 / embed 失败 → 降级 MySQL LIKE filename/summary（`service/ai_service.go`）

### 用户级 AI Provider 配置
- `/ai/config` CRUD + `/test` 连通性测试（对话 + embedding 双探测，返回维度校验）
- API key 用 AES-GCM 加密落库（`AI_CONFIG_SECRET`，未配置从 DSN 派生保证重启可解密），任何接口只回掩码
- baseURL 默认拒绝内网/回环（防 SSRF，`util/urlcheck.go`），`ALLOW_PRIVATE_AI_URL=true` 放行（本地 Ollama）
- 解析器 5min 内存缓存，保存/删除即失效；生效优先级：用户配置 → env 默认 → mock

### 可观测性
- 中间件顺序（不变量，勿调换）：`RequestIDMiddleware → MetricsMiddleware → gin.Recovery`
- 指标：`http_requests_total`（method/path/status）、`http_request_duration_seconds`、`file_upload_bytes_total`、`ai_tasks_total`、`ai_llm_duration_seconds`、`ai_index_ops_total`
- `/metrics` 端点（promhttp 默认注册表，含 go 运行时指标）；request_id 由 `ContextHandler` 自动附加到所有 slog 日志
- 访问日志：method/path(status 码)/latency/ip

## Testing

```bash
go test ./...           # 全量（无需外部依赖，全部绿）
go test -race ./...     # 竞态检测（CI 执行）
go test ./util/ -bench . -benchmem -run '^$'   # 基准（BENCHMARKS.md）
```

- repository 层测试用内存 SQLite（`glebarez/sqlite`），不依赖 MySQL
- 测试覆盖：`ai/` `handler/` `metrics/` `repository/` `service/` `storage/` `util/`

## Development Conventions

- 注释中英双语；导出标识符 PascalCase、未导出 camelCase
- 错误处理：I/O 操作必查 error；DB 写失败需回滚存储层（`store.Delete(ctx, key)`）；`slog` 结构化日志带 context（自动附加 request_id）
- handler 统一用 `respondError` 返回业务错误码，前端只判断 `code === 0`
- 测试用 `repository` 包内 `NewMockXxxRepository` + `httptest`，不依赖真实 MySQL/Redis
- 所有 handler 通过构造器注入依赖，禁止包级全局状态
- API 前缀：`/file`（鉴权）、`/user`、`/share`（公开限流）、`/ai/config`（鉴权）

## 常见任务

### 添加新 API
1. service 层实现业务方法（含所有权校验）
2. handler 层新增方法（`respondError` 统一错误码）+ Swagger 注解
3. `main.go` 注册路由（注意中间件：鉴权/限流）
4. 添加测试（mock repository + httptest）
5. 更新 README/README_CN API 表格；新增端点同步 docs/（`swag` 重新生成）

### 修改数据库表
1. `migrations/` 新增版本化迁移（`00000x_xxx.up.sql` + `.down.sql`，勿修改已发布迁移）
2. `model/` 同步 GORM 模型
3. `repository/` 对应 CRUD（全部带 ctx）
4. `schema.sql` 同步（作为初始化参考）

### 添加新存储后端
1. 实现 `storage.Storage` 全接口（含 `GetRange/FileSize/PresignPut/PresignGet`）
2. `main.go` 初始化 + fallback 策略
3. 添加对应测试

### 添加新 AI Provider
1. 实现 `ai.Provider`（`Analyze/Embed/Dimension`）
2. `ai/factory.go` 注册
3. 如需用户自定义，在 `AIConfigService.ResolveProvider` 中接入

### 前端
- 开发：`cd web && npm run dev`（Vite dev server）
- 构建：`npm run build`（vue-tsc 类型检查 + vite build，产物 `web/dist`）
- 测试：`npm test`（Vitest）

## 已知问题

- 🔴 `handler/handler.go` `UploadHandler` 的 FastUpload 短路分支：带 `filehash` 参数且存储层命中时直接返回"秒传成功"，**未为当前用户创建 `tbl_user_file` 关联行** → 用户看不到/下载不到该文件。`service.Upload` 内部秒传分支正确；修复应移除 handler 短路或改为走 service。
- 🟡 `web/dist` 未入库：本地/CI 需先 `cd web && npm run build`，否则 `/static` 404。
- 🟡 Range 开放区间（`bytes=N-`）在未知文件大小时返回 416。
- 🟡 分页为 `LIMIT/OFFSET`，大数据量深分页需改游标分页。

## Roadmap 状态

**已落地**：Phase 0 五项 Bug 修复、Phase 1 Redis（秒传缓存/分布式锁/全局限流）、Phase 2 预签名直传直下、Phase 3 Range + 分页、Phase 4 可观测性（Prometheus + request_id + Grafana dashboard）、Phase 6 AI（摘要/标签/语义搜索/相似/重复检测/用户级配置），以及分享、回收站、登出、统一错误码等增量功能；工程化：CI、Swagger、golang-migrate、BENCHMARKS、repository 测试（内存 SQLite）。

**待办**：
- Phase 5 剩余：覆盖率 80% 目标、MinIO 后端与并发压测、优化前后对比报告
- Phase 7：Kafka 异步任务、K8s 部署
- 上表"已知问题"修复
