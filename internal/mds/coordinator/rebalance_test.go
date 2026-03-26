package coordinator

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

func TestRebalancePlanner_ClassifyNodePressure(t *testing.T) {
	planner := NewRebalancePlanner(store.NewMemoryRepository(), RebalancePlannerConfig{
		HighWatermark: 0.85,
		LowWatermark:  0.60,
	})
	overfull, underfull := planner.classifyNodePressure([]metadata.NodeInfo{
		{ID: "node-over", Capacity: 100, Used: 90, Healthy: true, Address: "http://node-over.local"},
		{ID: "node-under", Capacity: 100, Used: 40, Healthy: true, Address: "http://node-under.local"},
		{ID: "node-mid", Capacity: 100, Used: 70, Healthy: true, Address: "http://node-mid.local"},
	})

	if len(overfull) != 1 || overfull[0].ID != "node-over" {
		t.Fatalf("expected node-over to be overfull, got %#v", overfull)
	}
	if len(underfull) != 1 || underfull[0].ID != "node-under" {
		t.Fatalf("expected node-under to be underfull, got %#v", underfull)
	}
}

func TestRebalancePlanner_PlanOnceCreatesPlanAndPendingReplica(t *testing.T) {
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
	now := time.Now().UTC()
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   100,
		Used:       90,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("update node-1 heartbeat: %v", err)
	}
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-2",
		Healthy:    true,
		Capacity:   100,
		Used:       70,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("update node-2 heartbeat: %v", err)
	}
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-3",
		Healthy:    true,
		Capacity:   100,
		Used:       10,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("update node-3 heartbeat: %v", err)
	}
	desired := 2
	policy := metadata.ReplicaPolicy{
		DesiredReplicaCount: 2,
		MinimumReplicaCount: 1,
		CurrentReplicaCount: 2,
	}
	if err := repo.UpdateChunkReplicas(context.Background(), store.ChunkReplicaPatch{
		Selector:      store.ChunkSelector{ID: "chunk-1"},
		ReplicaCount:  &desired,
		ReplicaPolicy: &policy,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("update chunk replica policy: %v", err)
	}

	planner := NewRebalancePlanner(repo, RebalancePlannerConfig{
		HighWatermark: 0.85,
		LowWatermark:  0.60,
	})
	if err := planner.PlanOnce(context.Background()); err != nil {
		t.Fatalf("plan once: %v", err)
	}

	plans, err := repo.ListReplicaPlans(context.Background(), store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeRebalance},
	})
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected one rebalance plan, got %d", len(plans))
	}
	if plans[0].SourceNodeID != "node-1" || plans[0].TargetNodeID != "node-3" {
		t.Fatalf("unexpected rebalance plan: %#v", plans[0])
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

func TestRebalancePlanner_PlanOnceSkipsChunkWithReplicaDeficit(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
	}, "node-1", "node-2", "node-3")
	now := time.Now().UTC()
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-1",
		Healthy:    true,
		Capacity:   100,
		Used:       90,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("update node-1 heartbeat: %v", err)
	}
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID:     "node-2",
		Healthy:    true,
		Capacity:   100,
		Used:       10,
		LastSeenAt: now,
	}); err != nil {
		t.Fatalf("update node-2 heartbeat: %v", err)
	}

	planner := NewRebalancePlanner(repo, RebalancePlannerConfig{
		HighWatermark: 0.85,
		LowWatermark:  0.60,
	})
	if err := planner.PlanOnce(context.Background()); err != nil {
		t.Fatalf("plan once: %v", err)
	}

	plans, err := repo.ListReplicaPlans(context.Background(), store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeRebalance},
	})
	if err != nil {
		t.Fatalf("list plans: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("expected no rebalance plan when chunk has replica deficit, got %d", len(plans))
	}
}
