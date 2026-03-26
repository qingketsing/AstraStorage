package store

import "context"

// BeginTx 通过深拷贝当前状态创建事务快照。
// 这个实现追求简单和稳定，更适合本地开发与单元测试，而不是高并发正式后端。
func (r *memoryRepository) BeginTx(context.Context) (Tx, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return &memoryTx{
		repo:  r,
		state: cloneState(r.state),
	}, nil
}

// InTx 封装最常见的事务使用方式：
// 成功则提交，回调报错则回滚并原样返回错误。
func (r *memoryRepository) InTx(ctx context.Context, fn func(context.Context, Tx) error) error {
	tx, err := r.BeginTx(ctx)
	if err != nil {
		return err
	}

	if err := fn(ctx, tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}

// Commit 把事务副本整体替换回主仓库。
// 当前实现不做版本检测，因此并发事务的冲突处理能力有限。
func (tx *memoryTx) Commit(context.Context) error {
	if tx.closed {
		return ErrConflict
	}

	tx.repo.mu.Lock()
	defer tx.repo.mu.Unlock()

	tx.repo.state = cloneState(tx.state)
	tx.closed = true
	return nil
}

// Rollback 只需要废弃当前事务副本，不需要回放任何反向操作。
func (tx *memoryTx) Rollback(context.Context) error {
	if tx.closed {
		return ErrConflict
	}

	tx.closed = true
	return nil
}
