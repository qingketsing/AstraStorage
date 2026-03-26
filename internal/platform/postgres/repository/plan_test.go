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

func TestCreateReplicaPlanRejectsDuplicateActiveTarget(t *testing.T) {
	now := time.Now().UTC()
	err := createReplicaPlan(context.Background(), fakeQueryDB{
		rowFn: func(_ context.Context, query string, _ ...any) rowScanner {
			if strings.Contains(query, "COUNT(*)") {
				return fakeRow{values: []any{int64(1)}}
			}
			return fakeRow{err: errors.New("unexpected query row")}
		},
	}, &metadata.ReplicaPlan{
		ID:            "plan-1",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-a",
		TargetNodeID:  "node-b",
		RequiredBytes: 4096,
		State:         metadata.ReplicaPlanStatePlanned,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestListReplicaPlansScansRows(t *testing.T) {
	now := time.Now().UTC()
	rows := &fakeRows{rows: [][]any{replicaPlanValues("plan-1", "chunk-1", "file-1", "node-a", "node-b", now)}}
	plans, err := listReplicaPlans(context.Background(), fakeQueryDB{
		queryFn: func(context.Context, string, ...any) (rowsScanner, error) {
			return rows, nil
		},
	}, store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeFailover},
	})
	if err != nil {
		t.Fatalf("list replica plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].ID != "plan-1" || plans[0].TargetNodeID != "node-b" {
		t.Fatalf("unexpected plan: %#v", plans[0])
	}
}

func TestUpdateReplicaPlanUsesResolvedID(t *testing.T) {
	now := time.Now().UTC()
	completedAt := now.Add(time.Minute)
	var execArgs []any
	err := updateReplicaPlan(context.Background(), fakeQueryDB{
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			execArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}, store.ReplicaPlanPatch{
		ID:          "plan-1",
		State:       replicaPlanStatePtr(metadata.ReplicaPlanStateDone),
		CompletedAt: &completedAt,
		UpdatedAt:   completedAt,
	})
	if err != nil {
		t.Fatalf("update replica plan: %v", err)
	}
	if got, want := execArgs[len(execArgs)-1], any("plan-1"); got != want {
		t.Fatalf("expected final where arg %v, got %#v", want, got)
	}
}

func replicaPlanValues(id string, chunkID metadata.ChunkID, fileID metadata.FileID, sourceNodeID, targetNodeID string, now time.Time) []any {
	return []any{
		id,
		string(metadata.ReplicaPlanTypeFailover),
		string(chunkID),
		string(fileID),
		sourceNodeID,
		targetNodeID,
		int64(4096),
		string(metadata.ReplicaPlanStatePlanned),
		0,
		"",
		"",
		0,
		sql.NullTime{},
		now,
		now,
		sql.NullTime{},
	}
}

func replicaPlanStatePtr(state metadata.ReplicaPlanState) *metadata.ReplicaPlanState {
	return &state
}
