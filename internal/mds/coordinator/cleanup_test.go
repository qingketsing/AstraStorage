package coordinator

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

func TestCleanupController_CleanupOnceFinalizesFailoverPlanAndPurgesSourceReplica(t *testing.T) {
	now := time.Now().UTC()
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-2": {
			NodeID:     "node-2",
			Role:       metadata.ReplicaRoleSecondary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-3": {
			NodeID:     "node-3",
			Role:       metadata.ReplicaRoleSecondary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
	}, "node-1", "node-2", "node-3")
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "failover-plan",
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-1",
		TargetNodeID:  "node-3",
		RequiredBytes: 16,
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create failover plan: %v", err)
	}

	controller := NewCleanupController(repo, CleanupControllerConfig{})
	if err := controller.CleanupOnce(context.Background()); err != nil {
		t.Fatalf("cleanup once: %v", err)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if _, ok := chunk.Replicas["node-1"]; ok {
		t.Fatalf("expected source replica metadata to be removed")
	}
	plan, err := repo.GetReplicaPlan(context.Background(), "failover-plan")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if plan.State != metadata.ReplicaPlanStateDone {
		t.Fatalf("expected done plan, got %q", plan.State)
	}
	if plan.CompletedAt == nil {
		t.Fatalf("expected completed_at to be set")
	}
}

func TestCleanupController_CleanupOnceDeletesSourceReplicaForRebalance(t *testing.T) {
	now := time.Now().UTC()
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-2": {
			NodeID:     "node-2",
			Role:       metadata.ReplicaRoleSecondary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-3": {
			NodeID:     "node-3",
			Role:       metadata.ReplicaRoleSecondary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
	}, "node-1", "node-2", "node-3")
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "rebalance-plan",
		Type:          metadata.ReplicaPlanTypeRebalance,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-1",
		TargetNodeID:  "node-3",
		RequiredBytes: 16,
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create rebalance plan: %v", err)
	}

	requestCount := 0
	controller := newCleanupController(repo, CleanupControllerConfig{}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if req.Method != http.MethodDelete {
			t.Fatalf("expected DELETE request, got %s", req.Method)
		}
		if req.URL.String() != "http://node-1.local/chunks/chunk-1" {
			t.Fatalf("unexpected delete request url: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})})
	if err := controller.CleanupOnce(context.Background()); err != nil {
		t.Fatalf("cleanup once: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected one delete request, got %d", requestCount)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if _, ok := chunk.Replicas["node-1"]; ok {
		t.Fatalf("expected source replica to be removed after rebalance cleanup")
	}
}

func TestCleanupController_CleanupOnceRetriesDeleteFailure(t *testing.T) {
	now := time.Now().UTC()
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-2": {
			NodeID:     "node-2",
			Role:       metadata.ReplicaRoleSecondary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-3": {
			NodeID:     "node-3",
			Role:       metadata.ReplicaRoleSecondary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
	}, "node-1", "node-2", "node-3")
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "rebalance-plan",
		Type:          metadata.ReplicaPlanTypeRebalance,
		ChunkID:       "chunk-1",
		FileID:        "file-1",
		SourceNodeID:  "node-1",
		TargetNodeID:  "node-3",
		RequiredBytes: 16,
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("create rebalance plan: %v", err)
	}

	controller := newCleanupController(repo, CleanupControllerConfig{
		RetryBackoff: time.Minute,
	}, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})})
	if err := controller.CleanupOnce(context.Background()); err != nil {
		t.Fatalf("cleanup once: %v", err)
	}

	plan, err := repo.GetReplicaPlan(context.Background(), "rebalance-plan")
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if plan.RetryCount != 1 {
		t.Fatalf("expected retry count 1, got %d", plan.RetryCount)
	}
	if plan.NextRetryAt == nil {
		t.Fatalf("expected next retry at to be set")
	}
	if plan.State != metadata.ReplicaPlanStateMaterialized {
		t.Fatalf("expected plan to stay materialized after retry scheduling, got %q", plan.State)
	}
}
