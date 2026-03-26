package repository

import (
	"context"
	"errors"
	"testing"

	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakePool struct {
	pingErr  error
	beginErr error
	tx       *fakeTx
	execFn   func(context.Context, string, ...any) (pgconn.CommandTag, error)
	queryFn  func(context.Context, string, ...any) (rowsScanner, error)
	rowFn    func(context.Context, string, ...any) rowScanner
}

func (f fakePool) Ping(context.Context) error {
	return f.pingErr
}

func (f fakePool) Begin(context.Context) (dbTx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

func (f fakePool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (f fakePool) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &fakeRows{}, nil
}

func (f fakePool) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	if f.rowFn != nil {
		return f.rowFn(ctx, sql, args...)
	}
	return fakeRow{err: errors.New("unexpected query row")}
}

type fakeTx struct {
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
	execFn      func(context.Context, string, ...any) (pgconn.CommandTag, error)
	queryFn     func(context.Context, string, ...any) (rowsScanner, error)
	rowFn       func(context.Context, string, ...any) rowScanner
}

func (f *fakeTx) Commit(context.Context) error {
	f.committed = true
	return f.commitErr
}

func (f *fakeTx) Rollback(context.Context) error {
	f.rolledBack = true
	return f.rollbackErr
}

func (f *fakeTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, sql, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeTx) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, sql, args...)
	}
	return &fakeRows{}, nil
}

func (f *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) rowScanner {
	if f.rowFn != nil {
		return f.rowFn(ctx, sql, args...)
	}
	return fakeRow{err: errors.New("unexpected query row")}
}

func TestRepositoryPingDelegatesToChecker(t *testing.T) {
	repo, err := newWithPool(fakePool{})
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	if err := repo.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestRepositoryInTxRollsBackOnCallbackError(t *testing.T) {
	want := errors.New("boom")
	fx := &fakeTx{}
	repo, err := newWithPool(fakePool{tx: fx})
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	err = repo.InTx(context.Background(), func(context.Context, store.Tx) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("expected callback error %v, got %v", want, err)
	}
	if !fx.rolledBack {
		t.Fatalf("expected rollback on callback error")
	}
	if fx.committed {
		t.Fatalf("did not expect commit on callback error")
	}
}

func TestRepositoryInTxCommitsOnSuccess(t *testing.T) {
	fx := &fakeTx{}
	repo, err := newWithPool(fakePool{tx: fx})
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	if err := repo.InTx(context.Background(), func(context.Context, store.Tx) error {
		return nil
	}); err != nil {
		t.Fatalf("in tx: %v", err)
	}
	if !fx.committed {
		t.Fatalf("expected commit on success")
	}
}
