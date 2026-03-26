package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUpsertChunksRejectsOffsetMismatch(t *testing.T) {
	now := time.Now().UTC()
	err := upsertChunks(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{values: fileValues("file-1", "inode-1", metadata.InodeID(metadata.RootInodeID), "/video.mp4", "video.mp4", now)}
		},
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return &fakeRows{}, nil
		},
	}, []metadata.ChunkMetadata{{
		ID:        "chunk-1",
		FileID:    "file-1",
		Index:     1,
		Offset:    0,
		Size:      128,
		Status:    metadata.ChunkStatusPersisted,
		CreatedAt: now,
		UpdatedAt: now,
	}})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestListChunksByFileLoadsReplicas(t *testing.T) {
	now := time.Now().UTC()
	chunkRows := &fakeRows{rows: [][]any{chunkValues("chunk-1", "file-1", 0, 0, now)}}
	replicaRows := &fakeRows{rows: [][]any{{
		"chunk-1",
		"node-a",
		"replica-1",
		"file-1",
		string(metadata.ReplicaRolePrimary),
		string(metadata.ReplicaStateReady),
		"sha256",
		"abc",
		true,
		sql.NullTime{Time: now, Valid: true},
		int64(128),
		now,
		now,
		sql.NullTime{Time: now, Valid: true},
	}}}
	chunks, err := listChunksByFile(context.Background(), fakeQueryDB{
		queryFn: func(_ context.Context, query string, _ ...any) (rowsScanner, error) {
			if strings.Contains(query, "mds_chunk_replicas") {
				return replicaRows, nil
			}
			return chunkRows, nil
		},
	}, "file-1")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 1 || len(chunks[0].Replicas) != 1 {
		t.Fatalf("expected chunk replicas to load, got %#v", chunks)
	}
}

func TestListChunksByFileLoadsReplicasForMultipleChunks(t *testing.T) {
	now := time.Now().UTC()
	chunkRows := &fakeRows{rows: [][]any{
		chunkValues("chunk-1", "file-1", 0, 0, now),
		chunkValues("chunk-2", "file-1", 1, 128, now),
	}}
	replicaRows := &fakeRows{rows: [][]any{
		{
			"chunk-1",
			"node-a",
			"replica-1a",
			"file-1",
			string(metadata.ReplicaRolePrimary),
			string(metadata.ReplicaStateReady),
			"",
			"",
			false,
			sql.NullTime{},
			int64(128),
			now,
			now,
			sql.NullTime{},
		},
		{
			"chunk-2",
			"node-b",
			"replica-2b",
			"file-1",
			string(metadata.ReplicaRolePrimary),
			string(metadata.ReplicaStateReady),
			"",
			"",
			false,
			sql.NullTime{},
			int64(128),
			now,
			now,
			sql.NullTime{},
		},
	}}

	chunks, err := listChunksByFile(context.Background(), fakeQueryDB{
		queryFn: func(_ context.Context, query string, _ ...any) (rowsScanner, error) {
			if strings.Contains(query, "mds_chunk_replicas") {
				return replicaRows, nil
			}
			return chunkRows, nil
		},
	}, "file-1")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if len(chunks[0].Replicas) != 1 {
		t.Fatalf("expected first chunk replicas to load, got %#v", chunks[0])
	}
	if len(chunks[1].Replicas) != 1 {
		t.Fatalf("expected second chunk replicas to load, got %#v", chunks[1])
	}
	if _, ok := chunks[0].Replicas["node-a"]; !ok {
		t.Fatalf("expected first chunk replica on node-a, got %#v", chunks[0].Replicas)
	}
	if _, ok := chunks[1].Replicas["node-b"]; !ok {
		t.Fatalf("expected second chunk replica on node-b, got %#v", chunks[1].Replicas)
	}
}

func TestListChunksByNodeLoadsChunksAndReplicas(t *testing.T) {
	now := time.Now().UTC()
	chunkRows := &fakeRows{rows: [][]any{
		chunkValues("chunk-1", "file-1", 0, 0, now),
		chunkValues("chunk-2", "file-2", 0, 0, now),
	}}
	replicaRows := &fakeRows{rows: [][]any{
		{
			"chunk-1",
			"node-a",
			"replica-1",
			"file-1",
			string(metadata.ReplicaRolePrimary),
			string(metadata.ReplicaStateReady),
			"",
			"",
			false,
			sql.NullTime{},
			int64(128),
			now,
			now,
			sql.NullTime{},
		},
		{
			"chunk-2",
			"node-a",
			"replica-2",
			"file-2",
			string(metadata.ReplicaRoleSecondary),
			string(metadata.ReplicaStateReady),
			"",
			"",
			false,
			sql.NullTime{},
			int64(128),
			now,
			now,
			sql.NullTime{},
		},
	}}
	chunks, err := listChunksByNode(context.Background(), fakeQueryDB{
		queryFn: func(_ context.Context, query string, _ ...any) (rowsScanner, error) {
			if strings.Contains(query, "WHERE chunk_id = ANY") {
				return replicaRows, nil
			}
			return chunkRows, nil
		},
	}, "node-a")
	if err != nil {
		t.Fatalf("list chunks by node: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if _, ok := chunks[0].Replicas["node-a"]; !ok {
		t.Fatalf("expected loaded replica on node-a, got %#v", chunks[0].Replicas)
	}
}

func TestRemoveChunkReplicaDeletesReplicaAndUpdatesCount(t *testing.T) {
	now := time.Now().UTC()
	rowCall := 0
	var execCalls []string
	err := removeChunkReplica(context.Background(), fakeQueryDB{
		rowFn: func(_ context.Context, query string, _ ...any) rowScanner {
			rowCall++
			switch {
			case strings.Contains(query, "FROM mds_chunks"):
				return fakeRow{values: chunkValues("chunk-1", "file-1", 0, 0, now)}
			case strings.Contains(query, "COUNT(*) FROM mds_chunk_replicas"):
				return fakeRow{values: []any{1}}
			default:
				return fakeRow{err: errors.New("unexpected query row")}
			}
		},
		queryFn: func(_ context.Context, query string, _ ...any) (rowsScanner, error) {
			if strings.Contains(query, "mds_chunk_replicas") {
				return &fakeRows{rows: [][]any{{
					"chunk-1",
					"node-a",
					"replica-1",
					"file-1",
					string(metadata.ReplicaRolePrimary),
					string(metadata.ReplicaStateReady),
					"",
					"",
					false,
					sql.NullTime{},
					int64(128),
					now,
					now,
					sql.NullTime{},
				}}}, nil
			}
			return &fakeRows{}, nil
		},
		execFn: func(_ context.Context, query string, _ ...any) (pgconn.CommandTag, error) {
			execCalls = append(execCalls, query)
			return pgconn.CommandTag{}, nil
		},
	}, store.ChunkSelector{ID: "chunk-1"}, "node-a", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("remove chunk replica: %v", err)
	}
	if rowCall == 0 {
		t.Fatalf("expected chunk lookup before delete")
	}
	if len(execCalls) != 2 {
		t.Fatalf("expected delete replica and update chunk count, got %d exec calls", len(execCalls))
	}
}

func chunkValues(id metadata.ChunkID, fileID metadata.FileID, index, offset int64, now time.Time) []any {
	return []any{
		string(id),
		string(fileID),
		index,
		offset,
		int64(128),
		string(metadata.ChunkStatusPersisted),
		int64(0),
		"",
		"",
		false,
		sql.NullTime{},
		0,
		0,
		0,
		0,
		now,
		now,
		sql.NullTime{},
		"",
	}
}
