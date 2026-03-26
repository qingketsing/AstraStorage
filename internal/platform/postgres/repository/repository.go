package repository

import (
	"context"
	"fmt"

	"AstraStorage/internal/mds/store"
	pghealth "AstraStorage/internal/platform/postgres/health"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rowScanner interface {
	Scan(dest ...any) error
}

type rowsScanner interface {
	Close()
	Err() error
	Next() bool
	Scan(dest ...any) error
}

type queryDB interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (rowsScanner, error)
	QueryRow(ctx context.Context, sql string, args ...any) rowScanner
}

type pool interface {
	queryDB
	Ping(ctx context.Context) error
	Begin(ctx context.Context) (dbTx, error)
}

type dbTx interface {
	queryDB
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type pgxPool struct {
	pool *pgxpool.Pool
}

type pgxTx struct {
	tx pgx.Tx
}

func (p pgxPool) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func (p pgxPool) Begin(ctx context.Context) (dbTx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return pgxTx{tx: tx}, nil
}

func (p pgxPool) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, arguments...)
}

func (p pgxPool) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	return p.pool.Query(ctx, sql, args...)
}

func (p pgxPool) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	return p.pool.QueryRow(ctx, sql, args...)
}

func (p pgxTx) Commit(ctx context.Context) error {
	return p.tx.Commit(ctx)
}

func (p pgxTx) Rollback(ctx context.Context) error {
	return p.tx.Rollback(ctx)
}

func (p pgxTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return p.tx.Exec(ctx, sql, arguments...)
}

func (p pgxTx) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	return p.tx.Query(ctx, sql, args...)
}

func (p pgxTx) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	return p.tx.QueryRow(ctx, sql, args...)
}

// Repository 是 PostgreSQL 版 Repository 的第一阶段实现。
// 当前阶段先提供健康检查、事务边界和接口占位，CRUD 在下一阶段补齐。
type Repository struct {
	unsupportedRepository
	pool    pool
	checker *pghealth.Checker
}

// New 使用 PostgreSQL 连接池构建 Repository。
func New(pool *pgxpool.Pool) (*Repository, error) {
	if pool == nil {
		return nil, fmt.Errorf("postgres repository: pool is nil")
	}
	checker, err := pghealth.NewChecker(pool)
	if err != nil {
		return nil, err
	}
	return &Repository{
		pool:    pgxPool{pool: pool},
		checker: checker,
	}, nil
}

func newWithPool(p pool) (*Repository, error) {
	if p == nil {
		return nil, fmt.Errorf("postgres repository: pool is nil")
	}
	checker, err := pghealth.NewChecker(p)
	if err != nil {
		return nil, err
	}
	return &Repository{
		pool:    p,
		checker: checker,
	}, nil
}

// Ping 探测数据库连通性。
func (r *Repository) Ping(ctx context.Context) error {
	return r.checker.Ping(ctx)
}

// BeginTx 开启一次 PostgreSQL 事务。
func (r *Repository) BeginTx(ctx context.Context) (store.Tx, error) {
	rawTx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres repository: begin tx: %w", err)
	}
	return &Tx{tx: rawTx}, nil
}

// InTx 在同一个事务边界内执行回调。
func (r *Repository) InTx(ctx context.Context, fn func(context.Context, store.Tx) error) error {
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

var _ store.Repository = (*Repository)(nil)
