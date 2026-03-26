package migrate

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRunner struct {
	execErr error
	tx      *fakeRunnerTx
}

func (f *fakeRunner) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.execErr
}

func (f *fakeRunner) Begin(context.Context) (runnerTx, error) {
	return f.tx, nil
}

type fakeRunnerTx struct {
	claimErr   error
	applyErr   error
	commitErr  error
	claimed    bool
	rolledBack bool
	committed  bool
}

func (f *fakeRunnerTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.applyErr
}

func (f *fakeRunnerTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeRow{err: f.claimErr, claimed: f.claimed}
}

func (f *fakeRunnerTx) Commit(context.Context) error {
	f.committed = true
	return f.commitErr
}

func (f *fakeRunnerTx) Rollback(context.Context) error {
	f.rolledBack = true
	return nil
}

type fakeRow struct {
	err     error
	claimed bool
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if !f.claimed {
		return pgx.ErrNoRows
	}
	*(dest[0].(*string)) = "001_init_mds.sql"
	return nil
}

func TestNewListIncludesInitialMigration(t *testing.T) {
	migrator, err := New()
	if err != nil {
		t.Fatalf("new migrator: %v", err)
	}

	files, err := migrator.List()
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	if len(files) == 0 || files[0] != "001_init_mds.sql" {
		t.Fatalf("expected initial migration to be present, got %v", files)
	}
}

func TestApplyMigrationSkipsAlreadyAppliedVersion(t *testing.T) {
	tx := &fakeRunnerTx{claimed: false}
	db := &fakeRunner{tx: tx}

	if err := applyMigration(context.Background(), db, "001_init_mds.sql", "SELECT 1;"); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if !tx.rolledBack {
		t.Fatalf("expected already-applied migration to roll back claim transaction")
	}
	if tx.committed {
		t.Fatalf("did not expect commit for already-applied migration")
	}
}

func TestApplyMigrationCommitsClaimedVersion(t *testing.T) {
	tx := &fakeRunnerTx{claimed: true}
	db := &fakeRunner{tx: tx}

	if err := applyMigration(context.Background(), db, "001_init_mds.sql", "SELECT 1;"); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	if !tx.committed {
		t.Fatalf("expected migration commit")
	}
}

func TestApplyMigrationReturnsClaimError(t *testing.T) {
	want := errors.New("claim failed")
	tx := &fakeRunnerTx{claimErr: want}
	db := &fakeRunner{tx: tx}

	if err := applyMigration(context.Background(), db, "001_init_mds.sql", "SELECT 1;"); !errors.Is(err, want) {
		t.Fatalf("expected wrapped error %v, got %v", want, err)
	}
}
