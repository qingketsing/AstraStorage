package repository

import (
	"context"
	"fmt"

	"AstraStorage/internal/mds/store"
)

// Tx 表示一条 PostgreSQL 事务上下文。
type Tx struct {
	unsupportedRepository
	tx     dbTx
	closed bool
}

// Commit 提交事务。
func (tx *Tx) Commit(ctx context.Context) error {
	if tx.closed {
		return store.ErrConflict
	}
	if err := tx.tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres repository: commit tx: %w", err)
	}
	tx.closed = true
	return nil
}

// Rollback 回滚事务。
func (tx *Tx) Rollback(ctx context.Context) error {
	if tx.closed {
		return store.ErrConflict
	}
	if err := tx.tx.Rollback(ctx); err != nil {
		return fmt.Errorf("postgres repository: rollback tx: %w", err)
	}
	tx.closed = true
	return nil
}

var _ store.Tx = (*Tx)(nil)
