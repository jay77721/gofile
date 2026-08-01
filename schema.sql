-- gofile 数据库初始化 Schema
-- 使用方法: mysql -u root -p gofile < migrations/000001_init_schema.up.sql

CREATE TABLE IF NOT EXISTS tbl_file (
  file_sha1 char(40) NOT NULL PRIMARY KEY COMMENT '文件 SHA1 哈希',
  user_name varchar(64) NOT NULL DEFAULT '' COMMENT '文件所有者',
  file_name varchar(256) NOT NULL DEFAULT '' COMMENT '文件名',
  file_size bigint(20) DEFAULT 0 COMMENT '文件大小(字节)',
  file_addr varchar(512) DEFAULT '' COMMENT '文件存储路径',
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
