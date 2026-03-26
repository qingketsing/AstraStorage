package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestUpsertNodeRejectsNegativeCapacity(t *testing.T) {
	err := upsertNode(context.Background(), fakeQueryDB{}, metadata.NodeInfo{
		ID:       "node-a",
		Capacity: -1,
	})
	if !errors.Is(err, store.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestUpdateNodeHeartbeatUpdatesExistingNode(t *testing.T) {
	now := time.Now().UTC()
	var execArgs []any
	err := updateNodeHeartbeat(context.Background(), fakeQueryDB{
		rowFn: func(context.Context, string, ...any) rowScanner {
			return fakeRow{values: nodeValues("node-a", now)}
		},
		execFn: func(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
			execArgs = append([]any(nil), args...)
			return pgconn.CommandTag{}, nil
		},
	}, store.NodeHeartbeatPatch{
		NodeID:     "node-a",
		Healthy:    true,
		Capacity:   100,
		Used:       50,
		LastSeenAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("update node heartbeat: %v", err)
	}
	if len(execArgs) != 5 || execArgs[4] != "node-a" {
		t.Fatalf("unexpected exec args: %#v", execArgs)
	}
}

func nodeValues(id metadata.NodeID, now time.Time) []any {
	return []any{
		string(id),
		"10.0.0.1",
		"rack-a",
		"zone-a",
		"region-a",
		mustJSON(map[string]string{"tier": "hot"}),
		int64(100),
		int64(10),
		true,
		sql.NullTime{Time: now, Valid: true},
		now,
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
