# gofile 2.0 工业级重构与升级实施方案 (M1 + M2 + M3)

> 目标：将 gofile 打造成具备工业级高并发处理能力、高可用异步调度中枢与树形虚拟文件系统（VFS）的高分项目。

---

## 🏗️ 总体架构演进

```
                        ┌────────────────────────────────────────────────────────┐
                        │               gofile 2.0 目标全景架构                   │
                        └────────────────────────────────────────────────────────┘
                                                     │
                ┌────────────────────────────────────┼────────────────────────────────────┐
                ▼                                    ▼                                    ▼
       【M1: 存储与分片直传】               【M2: VFS 虚拟文件系统】               【M3: 分布式任务调度】
       • MinIO S3 Multipart 协议            • 物化路径 (Materialized Path)        • 引入 hibiken/asynq
       • 零后端本地磁盘 I/O                 • 无限层级树形目录 & 批量移动         • 指数退避重试 & 死信队列
       • 并发预签名直传直合                 • 循环嵌套防护 & 递归 GC              • AI 分析解耦 & 独立 Worker
```

---

## 阶段一：M1 — S3 Multipart 预签名分片直传直合

### 1. 核心目标
- 彻底移除应用服务器本地 `ChunkDir` 与本地磁盘读写开销。
- 解决多实例/分布式部署时分片分散在不同节点导致合并失败的问题。
- 客户端与 MinIO 直连并发上传分片，应用服务器实现**零带宽、零 I/O 损耗**。

### 2. 数据库变更 (`tbl_multipart_upload`)
在 `migrations/000002_multipart_and_vfs.up.sql` 中创建表：

```sql
CREATE TABLE IF NOT EXISTS `tbl_multipart_upload` (
    `id` BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    `upload_id` VARCHAR(128) NOT NULL COMMENT 'S3/MinIO NewMultipartUpload 返回的 UploadID',
    `file_sha1` VARCHAR(40) NOT NULL COMMENT '目标文件完整 SHA1',
    `file_name` VARCHAR(256) NOT NULL COMMENT '原始文件名',
    `file_size` BIGINT NOT NULL COMMENT '文件总大小',
    `chunk_size` INT NOT NULL COMMENT '分片大小（默认 10MB）',
    `chunk_count` INT NOT NULL COMMENT '总分片数',
    `user_name` VARCHAR(64) NOT NULL COMMENT '所属用户名',
    `parent_id` BIGINT UNSIGNED DEFAULT 0 COMMENT '目标父文件夹 ID',
    `status` TINYINT DEFAULT 1 COMMENT '1:上传中 2:已完成 3:已取消',
    `create_at` DATETIME DEFAULT CURRENT_TIMESTAMP,
    `expired_at` DATETIME NOT NULL COMMENT '过期时间（24h）',
    UNIQUE KEY `uk_upload_id` (`upload_id`),
    INDEX `idx_user_sha1` (`user_name`, `file_sha1`),
    INDEX `idx_expired_at` (`expired_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 3. 核心交互流程
1. **秒传与初始化**：前端先根据文件 SHA1 调用 `/file/upload/multipart/init`。
   - 若系统已存在该 SHA1，直接秒传成功返回，无需传输数据。
   - 若未存在，后端调用 MinIO `NewMultipartUpload` 获取 `UploadID`，预先签发各 Part 的 `Presigned PUT URL` 返回给前端。
2. **客户端并发直传**：前端使用 `PUT` 请求直接将切片上传至 MinIO。
3. **完成合并**：前端收集所有分片的 `PartNumber` 和 `ETag`，调用 `/file/upload/multipart/complete`。
   - 后端调用 MinIO `CompleteMultipartUpload` 在存储层服务端原子合并。
   - 插入 `tbl_file`、`tbl_user_file`，投递异步分析任务。

### 4. 涉及代码变更
- `storage/storage.go`：扩展 `InitMultipart`、`PresignPartPut`、`CompleteMultipart`、`AbortMultipart` 接口。
- `storage/minio.go`：封装 MinIO Multipart API。
- `model/multipart.go`：定义数据模型与 DTO。
- `repository/multipart_repo.go`：实现 CRUD。
- `service/file_service.go`：增加初始化、批量签发与完成合并业务逻辑。
- `handler/handler.go`：增加对应 HTTP API。

---

## 阶段二：M2 — VFS 虚拟文件系统（树形目录与物化路径）

### 1. 核心目标
- 支持多级文件夹、目录树、面包屑导航、文件夹新建/重命名/移动（Move）与递归删除。
- 采用 **物化路径（Materialized Path）** 模式，保证查询任意子孙目录的高性能，并提供原子级路径重命名。

### 2. 数据库变更 (`tbl_user_file`)
```sql
ALTER TABLE `tbl_user_file`
    ADD COLUMN `parent_id` BIGINT UNSIGNED DEFAULT 0 COMMENT '父文件夹 ID，0 为根目录' AFTER `user_name`,
    ADD COLUMN `is_dir` TINYINT DEFAULT 0 COMMENT '0:文件, 1:文件夹' AFTER `parent_id`,
    ADD COLUMN `dir_path` VARCHAR(512) DEFAULT '/' COMMENT '物化路径，如 /学习资料/Go/' AFTER `is_dir`,
    ADD INDEX `idx_user_parent` (`user_name`, `parent_id`, `status`),
    ADD INDEX `idx_user_path` (`user_name`, `dir_path`(255));
```

### 3. 关键算法与业务规则
1. **循环移动检测（Circular Move Prevention）**：
   - 移动文件夹 $A$ 到目标文件夹 $B$ 时，必须校验 $B$ 的 `dir_path` 不能以 $A$ 的 `dir_path` 为前缀（禁止将父目录移到自身子孙目录下）。
2. **子孙路径原子前缀替换**：
   - 移动或重命名文件夹时，通过 SQL 字符串前缀替换更新该路径下的所有子节点：
     ```sql
     UPDATE tbl_user_file 
     SET dir_path = CONCAT(?, SUBSTRING(dir_path, ?))
     WHERE user_name = ? AND dir_path LIKE ?;
     ```
3. **递归软删除与恢复**：
   - 删除文件夹时，一并将其下所有子文件与子文件夹软删除；恢复时同理。

### 4. API 设计
- `POST /file/folder/create`：入参 `{ "name": "Go源码", "parent_id": 0 }`
- `POST /file/folder/rename`：入参 `{ "file_id": 12, "new_name": "Go进阶" }`
- `POST /file/folder/move`：入参 `{ "file_id": 12, "target_parent_id": 5 }`
- `GET /file/query`：升级支持 `parent_id` 参数，返回当前目录下文件/文件夹列表及 `breadcrumbs` 面包屑数据。

---

## 阶段三：M3 — 工业级分布式异步调度（Asynq + Redis）

### 1. 核心目标
- 替代进程内易丢失的 `chan Task` 内存队列，提升系统的容灾能力与工业级成熟度。
- 实现任务持久化、指数退避重试（Exponential Backoff）、死信队列（Dead Letter Queue）与 Worker 弹性伸缩。

### 2. 任务流转与架构
```
[HTTP 上传完成] ──▶ Asynq Client.Enqueue (Payload: sha1, filename, username)
                           │
                           ▼
                    [Redis 队列持久化]
                           │
                           ▼
                 [Asynq Worker Pool 消费]
                           │
           ┌───────────────┴───────────────┐
           ▼                               ▼
      [调用 LLM 分析]                [Typesense 混合索引]
           │ (失败)
           ▼
      [指数退避重试 (最多 3 次)] ──▶ (重试耗尽) ──▶ [进入死信队列 / 告警]
```

### 3. 代码结构规划 (`task/`)
```
task/
├── types.go       # 任务类型常量（TypeFileAIAnalyze 等）与 Payload 结构体
├── client.go      # 生产者客户端封装（Enqueue 方法）
├── server.go      # 消费者服务启动器、中间件（Recovery、Logger、Metrics）
└── processor.go   # 消费执行器：调用 AI Processor 完成提取、摘要与向量化
```

---

## 阶段四与阶段五（后续规划）

- **M4: 网盘知识库 RAG 智能问答（Chat with your Drive）**
  - 文档分块（Chunking）切片 + Dense&Sparse 混合召回 + RRF 融合。
  - Server-Sent Events (SSE) 流式打字机对话输出与溯源引用。
- **M5: WebDAV 协议支持**
  - 基于 `golang.org/x/net/webdav` 实现 `FileSystem`。
  - 支持 Windows 本地磁盘挂载、Mac Finder 挂载与 Infuse 播放器流媒体直链。

---

## 📋 执行任务追踪清单（Checklist）

- [ ] **Step 1**: 编写 `migrations/000002_multipart_and_vfs.up.sql` 并运行迁移验证。
- [ ] **Step 2**: 扩展 `storage.Storage` 接口并在 `storage/minio.go` 与 `storage/local.go` 中实现 S3 Multipart。
- [ ] **Step 3**: 创建 `model/multipart.go` 与 `repository/multipart_repo.go`。
- [ ] **Step 4**: 在 `service/file_service.go` 与 `handler/handler.go` 中接入分片初始化、直传预签名与合并 API。
- [ ] **Step 5**: 改造 `model/file.go`、`repository/file_repo.go` 与 `service/file_service.go`，实现 VFS 树形目录与物化路径。
- [ ] **Step 6**: 引入 `github.com/hibiken/asynq`，实现 `task/` 包并重构 AI 异步调度链路。
- [ ] **Step 7**: 编写对应的单元测试与集成测试，验证全链路正确性。
