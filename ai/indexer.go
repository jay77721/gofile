package ai

import "context"

// Doc 检索文档（对应 Typesense collection 的一条记录）
type Doc struct {
	ID         string
	Username   string
	Filehash   string
	Filename   string
	Summary    string
	Tags       []string
	Size       int64
	CreatedAt  int64
	ContentVec []float32
	Score      float64
}

// Indexer 检索引擎抽象（Typesense 实现 + 内存 mock 实现）
//
// 所有权隔离：SearchHybrid / Similar 的调用方已按 username 过滤，
// 派生索引永不绕过 tbl_user_file 的权限模型。
type Indexer interface {
	// EnsureCollection 幂等创建 collection（启动时调用）
	EnsureCollection(ctx context.Context) error
	// Upsert 写入/更新文档（幂等，id = username:filehash）
	Upsert(ctx context.Context, doc *Doc) error
	// Delete 删除文档
	Delete(ctx context.Context, username, filehash string) error
	// SearchHybrid 混合检索：全文 match filename/summary + 向量 KNN，RRF 融合
	SearchHybrid(ctx context.Context, q, username string, vector []float32, filter string, page, size int) ([]Doc, error)
	// Similar 相似文件推荐（向量 KNN，排除自身）
	Similar(ctx context.Context, username string, vector []float32, excludeFilehash string, limit int) ([]Doc, error)
	// DeleteByFilehash 按 filehash 删除所有用户的文档（GC 用）
	DeleteByFilehash(ctx context.Context, filehash string) error
}
