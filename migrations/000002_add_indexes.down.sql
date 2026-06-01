-- 回滚索引
DROP INDEX idx_file_status ON tbl_file;
DROP INDEX idx_file_create_at ON tbl_file;
DROP INDEX idx_token_expired_at ON tbl_user_token;
