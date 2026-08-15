-- gofile migration 000002: S3 Multipart Upload and Virtual File System (VFS)
-- 1. Create multipart upload table
CREATE TABLE IF NOT EXISTS tbl_multipart_upload (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  upload_id VARCHAR(128) NOT NULL COMMENT 'S3/MinIO NewMultipartUpload 返回的 UploadID',
  file_sha1 CHAR(40) NOT NULL COMMENT '目标文件完整 SHA1',
  file_name VARCHAR(256) NOT NULL COMMENT '原始文件名',
  file_size BIGINT NOT NULL COMMENT '文件总大小',
  chunk_size INT NOT NULL COMMENT '分片大小',
  chunk_count INT NOT NULL COMMENT '总分片数',
  user_name VARCHAR(64) NOT NULL COMMENT '所属用户名',
  parent_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '目标父目录ID',
  status TINYINT NOT NULL DEFAULT 1 COMMENT '1:上传中 2:已完成 3:已取消',
  create_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  expired_at DATETIME NOT NULL COMMENT '过期时间',
  PRIMARY KEY (id),
  UNIQUE KEY uk_upload_id (upload_id),
  INDEX idx_user_sha1 (user_name, file_sha1),
  INDEX idx_expired_at (expired_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='分片直传任务表';

-- 2. Enhance tbl_user_file for VFS (Virtual File System)
ALTER TABLE tbl_user_file
  DROP INDEX uk_user_file,
  ADD COLUMN parent_id BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '父文件夹 ID，0 为根目录' AFTER user_name,
  ADD COLUMN is_dir TINYINT(1) NOT NULL DEFAULT 0 COMMENT '0:文件, 1:文件夹' AFTER parent_id,
  ADD COLUMN dir_path VARCHAR(512) NOT NULL DEFAULT '/' COMMENT '物化路径，如 /学习资料/Go/' AFTER is_dir,
  ADD INDEX idx_user_sha1 (user_name, file_sha1),
  ADD INDEX idx_user_parent (user_name, parent_id, status),
  ADD INDEX idx_user_path (user_name, dir_path(255));
