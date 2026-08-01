package meta

import (
	mydb "gofile/db"
	"time"
)

// FileMeta 文件元信息结构
type FileMeta struct {
	FileSha1 string    `json:"filehash"`
	FileName string    `json:"filename"`
	FileSize int64     `json:"size"`
	Location string    `json:"-"`
	UploadAt time.Time `json:"upload_time"`
	Username string    `json:"username"`
}

// toFileMeta 将数据库 TableFile 转换为 FileMeta
func toFileMeta(tfile *mydb.TableFile) FileMeta {
	return FileMeta{
		FileSha1: tfile.FileSha1,
		FileName: tfile.FileName.String,
		FileSize: tfile.FileSize.Int64,
		Location: tfile.FileAddr.String,
		UploadAt: tfile.CreateAt.Time,
		Username: tfile.UserName.String,
	}
}

// toFileMetas 将数据库 TableFile 切片批量转换为 FileMeta 切片
func toFileMetas(tfiles []mydb.TableFile) []FileMeta {
	fmetas := make([]FileMeta, 0, len(tfiles))
	for _, tfile := range tfiles {
		fmetas = append(fmetas, toFileMeta(&tfile))
	}
	return fmetas
}

// UpdateFileMetaDB 新增/更新文件元到 MySQL
func UpdateFileMetaDB(fMeta FileMeta) bool {
	return mydb.OnFileUploadFinished(
		fMeta.FileSha1, fMeta.FileName, fMeta.FileSize, fMeta.Location, fMeta.Username, fMeta.UploadAt)
}

// GetFileMetaDB 从 MySQL 获取文件元信息
func GetFileMetaDB(fileSha1 string) (FileMeta, error) {
	tfile, err := mydb.GetFileMeta(fileSha1)
	if err != nil {
		return FileMeta{}, err
	}
	return toFileMeta(tfile), nil
}

// GetAllFileMetaDB 从 MySQL 获取所有文件元信息
func GetAllFileMetaDB() ([]FileMeta, error) {
	tfiles, err := mydb.GetAllFileMeta()
	if err != nil {
		return nil, err
	}
	return toFileMetas(tfiles), nil
}

// GetFileMetaDBByUser 从 MySQL 获取指定用户的文件元信息
func GetFileMetaDBByUser(fileSha1 string, username string) (FileMeta, error) {
	tfile, err := mydb.GetFileMetaByUser(fileSha1, username)
	if err != nil {
		return FileMeta{}, err
	}
	return toFileMeta(tfile), nil
}

// GetAllFileMetaDBByUser 从 MySQL 获取指定用户的所有文件元信息
func GetAllFileMetaDBByUser(username string) ([]FileMeta, error) {
	tfiles, err := mydb.GetAllFileMetaByUser(username)
	if err != nil {
		return nil, err
	}
	return toFileMetas(tfiles), nil
}

// DeleteFileMetaDB 软删除文件元信息
func DeleteFileMetaDB(fileSha1 string) bool {
	return mydb.DeleteFileMeta(fileSha1)
}

// UpdateFileMetaDBName 更新文件名
func UpdateFileMetaDBName(fileSha1 string, newFilename string) bool {
	return mydb.UpdateFileName(fileSha1, newFilename)
}
