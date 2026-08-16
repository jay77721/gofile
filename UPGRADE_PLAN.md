# gofile 2.0+ 工业级演进与全量升级实施方案

> 目标：将 gofile 打造成具备工业级高并发处理能力、高可用异步调度中枢、树形虚拟文件系统（VFS）、知识库 RAG 问答与 WebDAV 多端直连的高分项目。

---

## 🏗️ 总体架构演进全景

```
                        ┌────────────────────────────────────────────────────────┐
                        │               gofile 2.0+ 目标全景架构                  │
                        └────────────────────────────────────────────────────────┘
                                                     │
         ┌───────────────────┬───────────────────────┼───────────────────────┬───────────────────┐
         ▼                   ▼                       ▼                       ▼                   ▼
【M1: 存储分片直传】 【M2: VFS 虚拟文件系统】 【M3: 分布式任务调度】  【M4: 知识库 RAG 问答】 【M5: WebDAV 协议支持】
 • S3 Multipart 协议  • 物化路径 (Materialized) • hibiken/asynq 持久化  • 文档滑动窗口切片      • 标准 WebDAV FileSystem
 • 零后端本地磁盘 I/O  • 无限层级树形目录/改名/移动 • 指数退避与死信队列    • 多切片向量混合召回    • Windows/Mac 本地挂载
 • 并发预签名直传直合  • 循环嵌套防护 & 递归 GC  • 双路优雅降级容灾      • SSE 流式打字机对话    • Infuse 播放器流媒体直链
```

---

## 阶段一：M1 — S3 Multipart 预签名分片直传直合 ✅ (已完成 `c9d150f`)

- **核心目标**：彻底剥离应用服务器本地 `ChunkDir` 磁盘 I/O 与网络中转开销；解决多实例分布式合并失败。
- **数据库**：`tbl_multipart_upload` 表（记录 `upload_id`、`file_sha1`、`chunk_count`、`expired_at`）。
- **流程**：`init` 校验秒传或签发分片预签名 PUT URL → 前端直传 MinIO → `complete` 存储层服务端原子合并。

---

## 阶段二：M2 — VFS 虚拟文件系统（树形目录与物化路径） ✅ (已完成 `c9d150f`)

- **核心目标**：支持无限层级文件夹、目录树、面包屑导航、文件夹新建/重命名/移动（Move）与递归删除。
- **物化路径（Materialized Path）**：`tbl_user_file` 扩展 `parent_id`、`is_dir`、`dir_path`。
- **关键规则**：
  1. **防循环嵌套移入**：移动文件夹时校验目标路径不能包含源文件夹前缀。
  2. **原子批量前缀更新**：父目录移动/改名时通过 SQL `CONCAT(?, SUBSTRING(dir_path, ?))` 毫秒级批量更新子孙节点。

---

## 阶段三：M3 — 工业级分布式异步调度（Asynq + Redis） ✅ (已完成 `50d7309`)

- **核心目标**：替代单机内存 `chan taskItem`（容量 100），实现任务持久化、指数退避重试（MaxRetry=3）、死信队列与弹性 Worker。
- **结构设计**：`task/` 包封装 client, server, processor；多队列加权调度（`ai:6`, `default:3`）。
- **双路降级容灾**：`ASYNQ_ENABLED=true` 优先写 Redis；Redis 故障时自动降级进程内 chan。

---

## 阶段四：Step 1 — 代码健壮性与细节缺陷专项治理 ⏳ (当前重点)

- **4.1 消除 `ListByUser` 中的 N+1 查询隐患**：
  - `repository/file_repo.go` 将 for 循环逐行 `First(&f)` 改造为 `Where("file_sha1 IN ?", hashes)` 单条批量查询与内存 Map 组装。
- **4.2 完善 `main.go` 优雅停机（Graceful Shutdown）**：
  - 捕获 OS 退出信号时，联动调用 `asynqSrv.Shutdown()` 与 `aiProcessor.Stop()`，确保正在执行的 AI 任务平稳收尾。
- **4.3 新增 `tbl_multipart_upload` 挂起分片清理 Worker**：
  - `handler/cleanup.go` 新增定时任务，扫描 `status=1 AND expired_at < NOW()` 的上传会话，调用 `store.AbortMultipart` 清理 MinIO 遗留切片并置 `status=3`。
- **4.4 `ConfirmUpload` 预签名确认时校准真实文件大小**：
  - 调用存储层 `store.FileSize(ctx, fileHash)` 回填 `tbl_file.file_size`，修正默认写入 0 的问题。

---

## 阶段五：M4 — 网盘知识库 RAG 智能问答（Chat with your Drive） ⏳ (后续规划)

- **5.1 核心目标**：将网盘升级为个人/企业级知识库大脑，支持自然语言与文件对话。
- **5.2 链路设计**：
  1. **文档切片（Chunking）**：基于 `ai/extract.go` 提取文本，按 500~800 字符 + 100 字符重叠度分块。
  2. **多切片向量索引**：生成 Chunk 向量存入 Typesense，建立 `username:filehash:chunk_id` 文档。
  3. **召回与生成（RAG）**：提问语义召回 Top-K 相关切片 → 组装 Prompt → 传入 LLM。
  4. **流式输出**：Gin + Server-Sent Events (SSE) 接口 `GET /file/ai/chat`，前端打字机动态推送回答并附带引用文件。

---

## 阶段六：M5 — WebDAV 协议支持（本地磁盘挂载与多端直连） ⏳ (后续规划)

- **6.1 核心目标**：原生接入 Windows 资源管理器、Mac Finder 与 Infuse 视频播放器。
- **6.2 链路设计**：
  1. 基于 `golang.org/x/net/webdav` 实现 `webdav.FileSystem` 与 `webdav.File` 接口。
  2. 适配 VFS 物化路径与 Storage 读写流；支持 HTTP Basic Auth 认证。

---

## 阶段七：P5.2 & P7 — 质量保障与云原生容器编排 ⏳ (后续规划)

- **P5.2 测试覆盖率**：补充 VFS、Multipart、Asynq 专属测试，覆盖率提升至 ≥80%。
- **P7 云原生 K8s 部署**：Dockerfile 多阶段构建 + Helm Chart，拆分 API Pod 与 Worker Pod 独立弹性伸缩。

---

## 📋 执行任务追踪清单（Checklist）

### 已完成 (Done)
- [x] **M1**: S3 Multipart 预签名直传直合（`migrations/000002` + `model/multipart.go` + `storage/minio.go`）
- [x] **M2**: VFS 树形目录与物化路径（`parent_id` + `dir_path` + 防循环嵌套移动）
- [x] **M3**: Asynq 工业级分布式任务调度（`task/` 包 + 双路 Enqueue + `FastUpload` 补 enqueue）
- [x] **CI/CD**: GitHub Actions 三 Job 自动化流水线（gofmt + vet + test -race + build + docker）

### 待执行 (Next Actions)
- [x] **Step 1 (健壮性治理)**:
  - [x] 消除 `ListByUser` 中的 N+1 查询
  - [x] `main.go` 停机联动 `asynqSrv.Shutdown()` 与 `aiProcessor.Stop()`
  - [x] `cleanup.go` 新增过期挂起 Multipart 清理
  - [x] `ConfirmUpload` 校准实际文件大小
- [ ] **Step 2 (M4 RAG 知识库)**: 文档分块 + Typesense Chunk 向量索引 + SSE 流式问答
- [ ] **Step 3 (M5 WebDAV)**: 实现 `webdav.FileSystem` 协议层
- [ ] **Step 4 (P5.2 测试覆盖)**: 专项测试套件，测试覆盖率冲刺 ≥80%
