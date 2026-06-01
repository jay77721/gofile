-- 添加常用索引
-- 文件表：按状态查询频繁
CREATE INDEX idx_file_status ON tbl_file(status);

-- 文件表：按创建时间排序
CREATE INDEX idx_file_create_at ON tbl_file(create_at);

-- Token 表：按过期时间清理
CREATE INDEX idx_token_expired_at ON tbl_user_token(expired_at);
