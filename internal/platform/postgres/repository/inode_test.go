package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestBuildInodeSelectorWhereRejectsEmptySelector(t *testing.T) {
	_, _, err := buildInodeSelectorWhere(store.InodeSelector{})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestGetInodeReturnsScannedRecord(t *testing.T) {
	now := time.Now().UTC()
	got, err := getInode(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{values: []any{
				"docs",
				"root",
				"",
				"/docs",
				"docs",
				"directory",
				"active",
				int64(0),
				int64(0755),
				"alice",
				"dev",
				int64(1),
				int64(2),
				now,
				now,
				sql.NullTime{Time: now, Valid: true},
			}}
		},
	}, store.InodeSelector{ID: "docs"})
	if err != nil {
		t.Fatalf("get inode: %v", err)
	}
	if got.ID != "docs" || got.ParentID != "root" || got.Path != "/docs" {
		t.Fatalf("unexpected inode: %#v", got)
	}
	if got.AccessedAt == nil || !got.AccessedAt.Equal(now) {
		t.Fatalf("expected accessed_at to round-trip, got %#v", got.AccessedAt)
	}
}

func TestCreateInodeRejectsDuplicateSiblingName(t *testing.T) {
	now := time.Now().UTC()
	call := 0
	err := createInode(context.Background(), fakeQueryDB{
		rowFn: func(_ context.Context, query string, args ...any) rowScanner {
			call++
			switch call {
			case 1:
				return fakeRow{values: inodeValues(
					metadata.InodeID(metadata.RootInodeID),
					"",
					"",
					"/",
					"",
					metadata.InodeTypeDirectory,
					metadata.InodeStatusActive,
					now,
				)}
			case 2:
				return fakeRow{values: []any{"existing"}}
			default:
				return fakeRow{err: fmt.Errorf("unexpected query %q", strings.TrimSpace(query))}
			}
		},
	}, &metadata.InodeMetadata{
		ID:        "docs",
		ParentID:  metadata.InodeID(metadata.RootInodeID),
		Name:      "docs",
		Path:      "/docs",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestRenameInodeUpdatesNameAndPath(t *testing.T) {
	now := time.Now().UTC()
	var execArgs []any
	call := 0
	err := renameInode(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			call++
			switch call {
			case 1:
				return fakeRow{values: inodeValues("docs", "root", "", "/docs", "docs", metadata.InodeTypeDirectory, metadata.InodeStatusActive, now)}
			case 2:
				return fakeRow{err: pgx.ErrNoRows}
			default:
				return fakeRow{err: errors.New("unexpected query row")}
			}
		},
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			execArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}, store.RenameInodeOperation{
		Selector:  store.InodeSelector{ID: "docs"},
		NewName:   "manuals",
		UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("rename inode: %v", err)
	}
	if len(execArgs) != 4 {
		t.Fatalf("expected 4 exec args, got %d", len(execArgs))
	}
	if execArgs[0] != "manuals" || execArgs[1] != "/manuals" || execArgs[3] != "docs" {
		t.Fatalf("unexpected update args: %#v", execArgs)
	}
}

func TestMoveInodeRejectsDescendantTarget(t *testing.T) {
	now := time.Now().UTC()
	call := 0
	err := moveInode(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			call++
			switch call {
			case 1:
				return fakeRow{values: inodeValues("docs", "root", "", "/docs", "docs", metadata.InodeTypeDirectory, metadata.InodeStatusActive, now)}
			case 2:
				return fakeRow{values: inodeValues("nested", "docs", "", "/docs/nested", "nested", metadata.InodeTypeDirectory, metadata.InodeStatusActive, now)}
			case 3:
				return fakeRow{values: inodeValues("nested", "docs", "", "/docs/nested", "nested", metadata.InodeTypeDirectory, metadata.InodeStatusActive, now)}
			default:
				return fakeRow{err: errors.New("unexpected query row")}
			}
		},
	}, store.MoveInodeOperation{
		Selector:         store.InodeSelector{ID: "docs"},
		TargetParentID:   "nested",
		TargetParentPath: "/docs/nested",
		UpdatedAt:        now.Add(time.Minute),
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestUpdateSubtreePathsExecutesRecursiveUpdate(t *testing.T) {
	now := time.Now().UTC()
	var execSQL string
	var execArgs []any
	err := updateSubtreePaths(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{values: inodeValues("docs", "root", "", "/docs", "docs", metadata.InodeTypeDirectory, metadata.InodeStatusActive, now)}
		},
		execFn: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
			execSQL = query
			execArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}, store.UpdateSubtreePathsOperation{
		RootID:    "docs",
		OldPrefix: "/docs",
		NewPrefix: "/manuals",
		UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update subtree paths: %v", err)
	}
	if !strings.Contains(execSQL, "WITH RECURSIVE subtree") {
		t.Fatalf("expected recursive update query, got %q", execSQL)
	}
	if !reflect.DeepEqual(execArgs[:3], []any{"docs", "/manuals", "/docs"}) {
		t.Fatalf("unexpected subtree update args: %#v", execArgs)
	}
}

type fakeQueryDB struct {
	execFn  func(context.Context, string, ...any) (pgconn.CommandTag, error)
	queryFn func(context.Context, string, ...any) (rowsScanner, error)
	rowFn   func(context.Context, string, ...any) rowScanner
}

func (f fakeQueryDB) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if f.execFn != nil {
		return f.execFn(ctx, query, args...)
	}
	return pgconn.CommandTag{}, nil
}

func (f fakeQueryDB) Query(ctx context.Context, query string, args ...any) (rowsScanner, error) {
	if f.queryFn != nil {
		return f.queryFn(ctx, query, args...)
	}
	return &fakeRows{}, nil
}

func (f fakeQueryDB) QueryRow(ctx context.Context, query string, args ...any) rowScanner {
	if f.rowFn != nil {
		return f.rowFn(ctx, query, args...)
	}
	return fakeRow{err: errors.New("unexpected query row")}
}

type fakeRow struct {
	values []any
	err    error
}

func (f fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	return assignScannedValues(dest, f.values)
}

type fakeRows struct {
	rows [][]any
	err  error
	pos  int
}

func (f fakeRows) Close() {}

func (f fakeRows) Err() error { return f.err }

func (f *fakeRows) Next() bool {
	if f.pos >= len(f.rows) {
		return false
	}
	f.pos++
	return true
}

func (f *fakeRows) Scan(dest ...any) error {
	if f.pos == 0 || f.pos > len(f.rows) {
		return errors.New("fake rows: invalid scan position")
	}
	return assignScannedValues(dest, f.rows[f.pos-1])
}

func assignScannedValues(dest []any, values []any) error {
	if len(dest) != len(values) {
		return fmt.Errorf("scan arity mismatch: dest=%d values=%d", len(dest), len(values))
	}
	for i := range dest {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return fmt.Errorf("scan target %d is not a pointer", i)
		}
		elem := target.Elem()
		if values[i] == nil {
			elem.Set(reflect.Zero(elem.Type()))
			continue
		}
		value := reflect.ValueOf(values[i])
		if value.Type().AssignableTo(elem.Type()) {
			elem.Set(value)
			continue
		}
		if value.Type().ConvertibleTo(elem.Type()) {
			elem.Set(value.Convert(elem.Type()))
			continue
		}
		return fmt.Errorf("cannot assign %T to %s", values[i], elem.Type())
	}
	return nil
}

func inodeValues(id, parentID metadata.InodeID, fileID metadata.FileID, path, name string, inodeType metadata.InodeType, status metadata.InodeStatus, now time.Time) []any {
	return []any{
		string(id),
		string(parentID),
		string(fileID),
		path,
		name,
		string(inodeType),
		string(status),
		int64(0),
		int64(0),
		"",
		"",
		int64(1),
		int64(1),
		now,
		now,
		sql.NullTime{},
	}
}
