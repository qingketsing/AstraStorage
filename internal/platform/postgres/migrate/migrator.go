package migrate

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const createMigrationsTableSQL = `
CREATE TABLE IF NOT EXISTS mds_schema_migrations (
	version TEXT PRIMARY KEY,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

const claimMigrationSQL = `
INSERT INTO mds_schema_migrations(version)
VALUES ($1)
ON CONFLICT (version) DO NOTHING
RETURNING version;
`

//go:embed sql/*.sql
var embeddedMigrations embed.FS

type runner interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (runnerTx, error)
}

type runnerTx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

type poolRunner struct {
	pool *pgxpool.Pool
}

func (p poolRunner) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, arguments...)
}

func (p poolRunner) Begin(ctx context.Context) (runnerTx, error) {
	return p.pool.Begin(ctx)
}

// Migrator 负责执行内置的 PostgreSQL schema migration。
type Migrator struct {
	files fs.FS
}

// New 返回使用内置 migration 文件的迁移器。
func New() (*Migrator, error) {
	sub, err := fs.Sub(embeddedMigrations, "sql")
	if err != nil {
		return nil, fmt.Errorf("postgres migrate: load embedded migrations: %w", err)
	}
	return &Migrator{files: sub}, nil
}

// List 返回当前内置 migration 文件名，按字典序排序。
func (m *Migrator) List() ([]string, error) {
	if m == nil {
		return nil, fmt.Errorf("postgres migrate: migrator is nil")
	}
	entries, err := fs.ReadDir(m.files, ".")
	if err != nil {
		return nil, fmt.Errorf("postgres migrate: read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Up 执行所有尚未应用的 migration。
func (m *Migrator) Up(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return fmt.Errorf("postgres migrate: pool is nil")
	}
	return m.up(ctx, poolRunner{pool: pool})
}

func (m *Migrator) up(ctx context.Context, db runner) error {
	files, err := m.List()
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, createMigrationsTableSQL); err != nil {
		return fmt.Errorf("postgres migrate: ensure migration table: %w", err)
	}

	for _, name := range files {
		body, err := fs.ReadFile(m.files, name)
		if err != nil {
			return fmt.Errorf("postgres migrate: read %s: %w", name, err)
		}
		if err := applyMigration(ctx, db, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db runner, version string, sql string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres migrate: begin %s: %w", version, err)
	}

	claimed, err := claimMigration(ctx, tx, version)
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if !claimed {
		_ = tx.Rollback(ctx)
		return nil
	}

	if _, err := tx.Exec(ctx, sql); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("postgres migrate: apply %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres migrate: commit %s: %w", version, err)
	}
	return nil
}

func claimMigration(ctx context.Context, tx runnerTx, version string) (bool, error) {
	var claimed string
	err := tx.QueryRow(ctx, claimMigrationSQL, version).Scan(&claimed)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("postgres migrate: claim %s: %w", version, err)
	}
}
