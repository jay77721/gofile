package db

import (
	"database/sql"
	mydb "filestore-server/db/mysql"
	"log/slog"
	"time"
)

// OnFileUploadFinished 文件上传完成，保存元信息
func OnFileUploadFinished(filehash string, filename string, filesize int64, fileaddr string, username string, uploadAt time.Time) bool {
	stmt, err := mydb.DBConn().Prepare(
		"INSERT IGNORE INTO tbl_file(`file_sha1`,`user_name`,`file_name`,`file_size`,`file_addr`,`create_at`,status) VALUES(?,?,?,?,?,?,1)")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "OnFileUploadFinished")
		return false
	}
	defer stmt.Close()

	ret, err := stmt.Exec(filehash, username, filename, filesize, fileaddr, uploadAt)
	if err != nil {
		slog.Error("exec failed", "error", err, "op", "OnFileUploadFinished")
		return false
	}
	if rf, err := ret.RowsAffected(); err == nil {
		if rf <= 0 {
			slog.Info("file already uploaded", "filehash", filehash)
		}
		return true
	}
	return false
}

// TableFile 数据库文件记录结构
type TableFile struct {
	FileSha1 string
	UserName sql.NullString
	FileName sql.NullString
	FileSize sql.NullInt64
	FileAddr sql.NullString
	CreateAt sql.NullTime
}

// GetFileMeta 从 MySQL 获取文件元信息
func GetFileMeta(filehash string) (fileMeta *TableFile, err error) {
	stmt, err := mydb.DBConn().Prepare(
		"SELECT file_sha1, user_name, file_name, file_size, file_addr, create_at FROM tbl_file WHERE file_sha1=? AND status=1 LIMIT 1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "GetFileMeta")
		return nil, err
	}
	defer stmt.Close()

	tfile := TableFile{}
	err = stmt.QueryRow(filehash).Scan(&tfile.FileSha1, &tfile.UserName, &tfile.FileName, &tfile.FileSize, &tfile.FileAddr, &tfile.CreateAt)
	if err != nil {
		return nil, err
	}
	return &tfile, nil
}

// GetAllFileMeta 获取所有正常状态的文件元信息
func GetAllFileMeta() ([]TableFile, error) {
	rows, err := mydb.DBConn().Query(
		"SELECT file_sha1, user_name, file_name, file_size, file_addr, create_at FROM tbl_file WHERE status=1 ORDER BY create_at DESC")
	if err != nil {
		slog.Error("query all files failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var files []TableFile
	for rows.Next() {
		var tfile TableFile
		if err := rows.Scan(&tfile.FileSha1, &tfile.UserName, &tfile.FileName, &tfile.FileSize, &tfile.FileAddr, &tfile.CreateAt); err != nil {
			slog.Error("scan row failed", "error", err)
			continue
		}
		files = append(files, tfile)
	}
	return files, nil
}

// GetFileMetaByUser 获取指定用户的文件元信息
func GetFileMetaByUser(filehash string, username string) (fileMeta *TableFile, err error) {
	stmt, err := mydb.DBConn().Prepare(
		"SELECT file_sha1, user_name, file_name, file_size, file_addr, create_at FROM tbl_file WHERE file_sha1=? AND user_name=? AND status=1 LIMIT 1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "GetFileMetaByUser")
		return nil, err
	}
	defer stmt.Close()

	tfile := TableFile{}
	err = stmt.QueryRow(filehash, username).Scan(&tfile.FileSha1, &tfile.UserName, &tfile.FileName, &tfile.FileSize, &tfile.FileAddr, &tfile.CreateAt)
	if err != nil {
		return nil, err
	}
	return &tfile, nil
}

// GetAllFileMetaByUser 获取指定用户的所有正常状态文件
func GetAllFileMetaByUser(username string) ([]TableFile, error) {
	rows, err := mydb.DBConn().Query(
		"SELECT file_sha1, user_name, file_name, file_size, file_addr, create_at FROM tbl_file WHERE user_name=? AND status=1 ORDER BY create_at DESC", username)
	if err != nil {
		slog.Error("query all files by user failed", "error", err, "username", username)
		return nil, err
	}
	defer rows.Close()

	var files []TableFile
	for rows.Next() {
		var tfile TableFile
		if err := rows.Scan(&tfile.FileSha1, &tfile.UserName, &tfile.FileName, &tfile.FileSize, &tfile.FileAddr, &tfile.CreateAt); err != nil {
			slog.Error("scan row failed", "error", err)
			continue
		}
		files = append(files, tfile)
	}
	return files, nil
}

// DeleteFileMeta 软删除文件元信息（status 设为 2）
func DeleteFileMeta(filehash string) bool {
	stmt, err := mydb.DBConn().Prepare("UPDATE tbl_file SET status=2 WHERE file_sha1=? AND status=1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "DeleteFileMeta")
		return false
	}
	defer stmt.Close()

	ret, err := stmt.Exec(filehash)
	if err != nil {
		slog.Error("exec failed", "error", err, "op", "DeleteFileMeta")
		return false
	}

	rowsAffected, _ := ret.RowsAffected()
	return rowsAffected > 0
}

// UpdateFileName 更新文件名
func UpdateFileName(filehash string, newFilename string) bool {
	stmt, err := mydb.DBConn().Prepare("UPDATE tbl_file SET file_name=? WHERE file_sha1=? AND status=1")
	if err != nil {
		slog.Error("prepare statement failed", "error", err, "op", "UpdateFileName")
		return false
	}
	defer stmt.Close()

	_, err = stmt.Exec(newFilename, filehash)
	if err != nil {
		slog.Error("exec failed", "error", err, "op", "UpdateFileName")
		return false
	}
	return true
}
