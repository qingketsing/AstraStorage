// txn.go
// Metadata Service 元数据事务操作定义文件。
// 该文件预留用于描述元数据更新过程中的事务语义，
// 包括原子写入、一致性控制、回滚处理以及多步骤操作的提交边界管理。

package store

import "context"

// TransactionManager 定义元数据事务的开启与执行接口。
type TransactionManager interface {
	BeginTx(ctx context.Context) (Tx, error)
	InTx(ctx context.Context, fn func(context.Context, Tx) error) error
}

// Tx 表示一次元数据事务上下文。
// 事务内暴露与 Repository 一致的读写能力，便于调用方在单个边界内完成文件、
// chunk、节点与上传会话的原子更新。
type Tx interface {
	InodeRepository
	FileRepository
	ChunkRepository
	UploadSessionRepository
	NodeRepository
	ReplicaPlanRepository

	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}
