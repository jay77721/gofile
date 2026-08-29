-- gofile 数据库初始化 Schema
-- 唯一真相源:新部署执行本文件即可;表结构变更请同步修改本文件与 db/mysql AutoMigrate 模型
-- 使用方法: mysql -u root -p gofile < schema.sql

CREATE TABLE IF NOT EXISTS tbl_user_file (
  id BIGINT NOT NULL AUTO_INCREMENT,
  user_name varchar(64) NOT NULL,
  parent_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '父文件夹 ID，0 为根目录',
  is_dir tinyint(1) NOT NULL DEFAULT 0 COMMENT '0:文件, 1:文件夹',
  dir_path varchar(512) NOT NULL DEFAULT '/' COMMENT '物化路径，例如 /Go/Sources/',
  file_sha1 char(40) NOT NULL,
  file_name varchar(256) NOT NULL DEFAULT '',
  status tinyint(4) NOT NULL DEFAULT 1 COMMENT '状态: 1-拥有(正常), 2-已删除',
  create_at datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  INDEX idx_user_file_status (status),
  INDEX idx_user_sha1 (user_name, file_sha1),
  INDEX idx_user_parent (user_name, parent_id, status),
  INDEX idx_user_path (user_name, dir_path(255))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户文件拥有关系表';

CREATE TABLE IF NOT EXISTS tbl_file (
  file_sha1 char(40) NOT NULL PRIMARY KEY COMMENT '文件 SHA1 哈希',
  user_name varchar(64) NOT NULL DEFAULT '' COMMENT '文件所有者',
  file_name varchar(256) NOT NULL DEFAULT '' COMMENT '文件名',
  file_size bigint(20) DEFAULT 0 COMMENT '文件大小(字节)',
  file_addr varchar(512) DEFAULT '' COMMENT '文件存储路径',
  file_summary TEXT DEFAULT NULL COMMENT 'AI 生成的内容摘要',
  tags varchar(255) DEFAULT '' COMMENT 'AI 生成的标签，逗号分隔',
  create_at datetime DEFAULT CURRENT_TIMESTAMP COMMENT '上传时间',
  status tinyint(4) NOT NULL DEFAULT 0 COMMENT '状态: 0-正常, 1-已删除, 2-禁止'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='文件元信息表';

CREATE INDEX idx_file_status ON tbl_file(status);
CREATE INDEX idx_file_create_at ON tbl_file(create_at);

CREATE TABLE IF NOT EXISTS tbl_user (
  user_name varchar(64) NOT NULL PRIMARY KEY COMMENT '用户名',
  user_pwd varchar(60) NOT NULL DEFAULT '' COMMENT '密码(bcrypt 哈希)',
  signup_at datetime DEFAULT CURRENT_TIMESTAMP COMMENT '注册时间',
  status tinyint(4) NOT NULL DEFAULT 0 COMMENT '状态: 0-正常, 1-禁用'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户表';

CREATE TABLE IF NOT EXISTS tbl_user_token (
  user_name varchar(64) NOT NULL PRIMARY KEY COMMENT '用户名',
  user_token char(64) NOT NULL DEFAULT '' COMMENT '会话 Token',
  update_at datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT '更新时间',
  expired_at datetime COMMENT '过期时间'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户会话 Token 表';

CREATE INDEX idx_token_expired_at ON tbl_user_token(expired_at);

-- AI 异步分析任务状态机
-- 状态: 0-pending, 1-processing, 2-done, 3-failed(可补偿重试)
CREATE TABLE IF NOT EXISTS tbl_ai_task (
  id BIGINT NOT NULL AUTO_INCREMENT,
  file_sha1 char(40) NOT NULL,
  user_name varchar(64) NOT NULL,
  status tinyint(4) NOT NULL DEFAULT 0,
  retry_count INT NOT NULL DEFAULT 0,
  error_msg varchar(512) DEFAULT '',
  expired_at datetime DEFAULT (CURRENT_TIMESTAMP + INTERVAL 7 DAY) COMMENT '任务过期时间（7天后可清理）',
  create_at datetime DEFAULT CURRENT_TIMESTAMP,
  update_at datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_sha1_user (file_sha1, user_name),
  INDEX idx_status (status),
  INDEX idx_task_expired_at (expired_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='AI 异步分析任务表';

-- 文件分享表
CREATE TABLE IF NOT EXISTS tbl_share (
  id BIGINT NOT NULL AUTO_INCREMENT,
  share_token varchar(64) NOT NULL COMMENT '分享令牌',
  file_sha1 char(40) NOT NULL COMMENT '分享的文件 hash',
  user_name varchar(64) NOT NULL COMMENT '分享者',
  password_hash varchar(255) NOT NULL DEFAULT '' COMMENT '提取码 bcrypt 哈希,空为无密码',
  expire_at datetime NOT NULL COMMENT '过期时间',
  create_at datetime DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_share_token (share_token),
  INDEX idx_share_file (file_sha1),
  INDEX idx_share_expire (expire_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='文件分享表';

-- 用户级 AI Provider 配置(前端自定义 OpenAI 协议 baseURL/API key)
CREATE TABLE IF NOT EXISTS tbl_ai_config (
  user_name varchar(64) NOT NULL PRIMARY KEY COMMENT '用户名',
  base_url varchar(512) NOT NULL DEFAULT '' COMMENT 'OpenAI 协议端点,空为系统默认',
  api_key_enc varchar(512) NOT NULL DEFAULT '' COMMENT 'API Key AES-GCM 密文(base64),不下发明文',
  model varchar(128) NOT NULL DEFAULT '' COMMENT '对话模型名',
  embed_model varchar(128) NOT NULL DEFAULT '' COMMENT 'embedding 模型名',
  update_at datetime DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='用户 AI Provider 配置表';

-- S3/MinIO multipart upload sessions. This table is the fresh-install
-- equivalent of migrations/000002_multipart_and_vfs.up.sql.
CREATE TABLE IF NOT EXISTS tbl_multipart_upload (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  upload_id varchar(128) NOT NULL,
  file_sha1 char(40) NOT NULL,
  file_name varchar(256) NOT NULL,
  file_size BIGINT NOT NULL,
  chunk_size INT NOT NULL,
  chunk_count INT NOT NULL,
  user_name varchar(64) NOT NULL,
  parent_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  status tinyint NOT NULL DEFAULT 1 COMMENT '1:上传中, 2:已完成, 3:已取消',
  create_at datetime DEFAULT CURRENT_TIMESTAMP,
  expired_at datetime NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_upload_id (upload_id),
  INDEX idx_user_sha1 (user_name, file_sha1),
  INDEX idx_expired_at (expired_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='S3/MinIO 分片直传会话表';
