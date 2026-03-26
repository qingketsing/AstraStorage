package coordinator

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

func TestFailoverPlanner_ListUnavailableNodesUsesHeartbeatTimeout(t *testing.T) {
	repo := store.NewMemoryRepository()
	now := time.Now().UTC()
	staleSeen := now.Add(-10 * time.Minute)
	freshSeen := now.Add(-time.Minute)

	for _, node := range []metadata.NodeInfo{
		{ID: "node-stale", Address: "http://node-stale.local", Capacity: 1024, Healthy: true, LastSeenAt: &staleSeen, UpdatedAt: staleSeen},
		{ID: "node-fresh", Address: "http://node-fresh.local", Capacity: 1024, Healthy: true, LastSeenAt: &freshSeen, UpdatedAt: freshSeen},
	} {
		if err := repo.UpsertNode(context.Background(), node); err != nil {
			t.Fatalf("upsert node %s: %v", node.ID, err)
		}
	}

	planner := NewFailoverPlanner(repo, FailoverPlannerConfig{
		NodeTimeout: time.Minute * 5,
	})
	nodes, err := planner.listUnavailableNodes(context.Background(), now)
	if err != nil {
		t.Fatalf("list unavailable nodes: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-stale" {
		t.Fatalf("expected only stale node to be unavailable, got %#v", nodes)
	}
}

func TestFailoverPlanner_PlanOnceCreatesPlanAndPendingReplica(t *testing.T) {
	now := time.Now().UTC()
	staleSeen := now.Add(-10 * time.Minute)
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
	}, "node-1", "node-2", "node-3")
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   1024,
		Used:       0,
		LastSeenAt: staleSeen,
	}); err != nil {
		t.Fatalf("mark node-1 stale: %v", err)
	}

	planner := NewFailoverPlanner(repo, FailoverPlannerConfig{
		NodeTimeout: time.Minute * 5,
	})
	if err := planner.PlanOnce(context.Background()); err != nil {
		t.Fatalf("plan once: %v", err)
	}

	plans, err := repo.ListReplicaPlans(context.Background(), store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeFailover},
	})
	if err != nil {
		t.Fatalf("list replica plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 failover plan, got %d", len(plans))
	}
	if plans[0].TargetNodeID != "node-3" {
		t.Fatalf("expected node-3 target, got %q", plans[0].TargetNodeID)
	}
	if plans[0].State != metadata.ReplicaPlanStateMaterialized {
		t.Fatalf("expected materialized plan, got %q", plans[0].State)
	}

	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	replica, ok := chunk.Replicas["node-3"]
	if !ok {
		t.Fatalf("expected pending replica on node-3")
	}
	if replica.State != metadata.ReplicaStatePending {
		t.Fatalf("expected pending replica state, got %q", replica.State)
	}
}

func TestFailoverPlanner_PlanOnceSkipsChunkWithActivePlan(t *testing.T) {
	now := time.Now().UTC()
	staleSeen := now.Add(-10 * time.Minute)
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
	}, "node-1", "node-2", "node-3")
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   1024,
		Used:       0,
		LastSeenAt: staleSeen,
	}); err != nil {
		t.Fatalf("mark node-1 stale: %v", err)
	}
	if err := repo.CreateReplicaPlan(context.Background(), &metadata.ReplicaPlan{
		ID:            "existing-plan",
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
		t.Fatalf("create existing plan: %v", err)
	}

	planner := NewFailoverPlanner(repo, FailoverPlannerConfig{
		NodeTimeout: time.Minute * 5,
	})
	if err := planner.PlanOnce(context.Background()); err != nil {
		t.Fatalf("plan once: %v", err)
	}

	plans, err := repo.ListReplicaPlans(context.Background(), store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeFailover},
	})
	if err != nil {
		t.Fatalf("list replica plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected planner to keep single active plan, got %d", len(plans))
	}
	chunk, err := repo.GetChunk(context.Background(), store.ChunkSelector{ID: "chunk-1"})
	if err != nil {
		t.Fatalf("get chunk: %v", err)
	}
	if _, ok := chunk.Replicas["node-3"]; ok {
		t.Fatalf("did not expect planner to materialize a second pending replica")
	}
}
