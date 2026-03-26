package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestBuildFileSelectorWhereRejectsEmptySelector(t *testing.T) {
	_, _, err := buildFileSelectorWhere(store.FileSelector{})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestCreateFileRejectsDirectoryInode(t *testing.T) {
	now := time.Now().UTC()
	err := createFile(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{values: inodeValues("docs", metadata.InodeID(metadata.RootInodeID), "", "/docs", "docs", metadata.InodeTypeDirectory, metadata.InodeStatusActive, now)}
		},
	}, &metadata.FileMetadata{
		ID:        "file-1",
		InodeID:   "docs",
		Path:      "/docs",
		Name:      "docs",
		Status:    metadata.FileStatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestCreateFileRejectsSecondFileForSameInode(t *testing.T) {
	now := time.Now().UTC()
	rowCall := 0
	err := createFile(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			rowCall++
			switch rowCall {
			case 1:
				return fakeRow{values: inodeValues("readme", metadata.InodeID(metadata.RootInodeID), "", "/readme.txt", "readme.txt", metadata.InodeTypeFile, metadata.InodeStatusActive, now)}
			case 2:
				return fakeRow{values: fileValues("file-1", "readme", metadata.InodeID(metadata.RootInodeID), "/readme.txt", "readme.txt", now)}
			default:
				return fakeRow{err: errors.New("unexpected query row")}
			}
		},
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return &fakeRows{}, nil
		},
	}, &metadata.FileMetadata{
		ID:            "file-2",
		InodeID:       "readme",
		ParentInodeID: metadata.InodeID(metadata.RootInodeID),
		Path:          "/readme.txt",
		Name:          "readme.txt",
		Status:        metadata.FileStatusPending,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestUpdateFileUsesResolvedFileID(t *testing.T) {
	now := time.Now().UTC()
	rowCall := 0
	var execArgs []any
	err := updateFile(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			rowCall++
			if rowCall == 1 {
				return fakeRow{values: fileValues("file-1", "inode-1", metadata.InodeID(metadata.RootInodeID), "/a.txt", "a.txt", now)}
			}
			return fakeRow{err: errors.New("unexpected query row")}
		},
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return &fakeRows{}, nil
		},
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			execArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}, store.FilePatch{
		Selector:  store.FileSelector{Path: "/a.txt"},
		Path:      strPtr("/manuals/a.txt"),
		Name:      strPtr("manual-a.txt"),
		UpdatedAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update file: %v", err)
	}
	if got, want := execArgs[len(execArgs)-1], any("file-1"); got != want {
		t.Fatalf("expected final where arg %v, got %#v", want, got)
	}
}

func TestListFilesLoadsPlacements(t *testing.T) {
	now := time.Now().UTC()
	filesRows := &fakeRows{
		rows: [][]any{
			fileValues("file-1", "inode-1", metadata.InodeID(metadata.RootInodeID), "/notes.txt", "notes.txt", now),
		},
	}
	labels, _ := json.Marshal(map[string]string{"tier": "hot"})
	chunkIDs, _ := json.Marshal([]metadata.ChunkID{"chunk-1"})
	placementRows := &fakeRows{
		rows: [][]any{
			{
				"file-1",
				"node-a",
				string(metadata.ReplicaRolePrimary),
				string(metadata.ReplicaStateReady),
				true,
				chunkIDs,
				int64(128),
				"verified",
				sql.NullTime{Time: now, Valid: true},
				"node-a",
				"10.0.0.1",
				"rack-a",
				"zone-a",
				"region-a",
				labels,
				int64(1000),
				int64(128),
				true,
				sql.NullTime{Time: now, Valid: true},
				sql.NullTime{Time: now, Valid: true},
			},
		},
	}
	queryCall := 0
	files, err := listFiles(context.Background(), fakeQueryDB{
		queryFn: func(_ context.Context, query string, _ ...any) (rowsScanner, error) {
			queryCall++
			if strings.Contains(query, "FROM mds_file_placements") {
				return placementRows, nil
			}
			return filesRows, nil
		},
	}, store.FileFilter{PathPrefix: "/notes"})
	if err != nil {
		t.Fatalf("list files: %v", err)
	}
	if queryCall < 2 {
		t.Fatalf("expected file and placement queries, got %d", queryCall)
	}
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d", len(files))
	}
	file := files[0]
	placement, ok := file.NodePlacements["node-a"]
	if !ok {
		t.Fatalf("expected placement for node-a, got %#v", file.NodePlacements)
	}
	if placement.Node.Address != "10.0.0.1" || !reflect.DeepEqual(placement.ChunkIDs, []metadata.ChunkID{"chunk-1"}) {
		t.Fatalf("unexpected placement: %#v", placement)
	}
}

func fileValues(fileID metadata.FileID, inodeID metadata.InodeID, parentID metadata.InodeID, path, name string, now time.Time) []any {
	secondaryNodeIDs, _ := json.Marshal([]metadata.NodeID{})
	userMetadata, _ := json.Marshal(map[string]string{})
	tags, _ := json.Marshal(map[string]string{})
	return []any{
		string(fileID),
		"",
		string(inodeID),
		string(parentID),
		path,
		name,
		int64(0),
		int64(0),
		metadata.FixedChunkSizeBytes,
		int64(0),
		string(metadata.FileStatusPending),
		"",
		"",
		"",
		secondaryNodeIDs,
		"",
		"",
		"",
		false,
		sql.NullTime{},
		0,
		0,
		0,
		userMetadata,
		tags,
		now,
		now,
		sql.NullTime{},
	}
}

func strPtr(v string) *string {
	return &v
}
