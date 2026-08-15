-- Rollback gofile migration 000002

DROP TABLE IF EXISTS tbl_multipart_upload;

ALTER TABLE tbl_user_file
  DROP INDEX idx_user_parent,
  DROP INDEX idx_user_path,
  DROP INDEX idx_user_sha1,
  DROP COLUMN parent_id,
  DROP COLUMN is_dir,
  DROP COLUMN dir_path,
  ADD UNIQUE KEY uk_user_file (user_name, file_sha1);
