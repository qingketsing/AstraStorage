package coordinator

import (
	"context"
	"testing"
	"time"

	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/mq/contracts"
)

func TestPendingReplicaRepairer_RepairOncePublishesRepairTask(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {
			NodeID:     "node-1",
			Role:       metadata.ReplicaRolePrimary,
			State:      metadata.ReplicaStateReady,
			StoredSize: 16,
		},
		"node-2": {
			NodeID: "node-2",
			Role:   metadata.ReplicaRoleSecondary,
			State:  metadata.ReplicaStatePending,
		},
	}, "node-1", "node-2")
	producer := &capturingTaskProducer{}
	repairer, err := newPendingReplicaRepairer(repo, PendingReplicaRepairerConfig{
		Interval:          time.Second,
		RetryBackoff:      time.Minute,
		MaxReplicasPerRun: 8,
	}, nil)
	if err != nil {
		t.Fatalf("new repairer: %v", err)
	}
	repairer.SetTaskProducer(producer)

	if err := repairer.RepairOnce(context.Background()); err != nil {
		t.Fatalf("repair once: %v", err)
	}
	if len(producer.repairTasks) != 1 {
		t.Fatalf("expected one repair task, got %d", len(producer.repairTasks))
	}
	if producer.repairTasks[0].ChunkID != "chunk-1" || producer.repairTasks[0].TargetNodeID != "node-2" {
		t.Fatalf("unexpected repair task %#v", producer.repairTasks[0])
	}
}

func TestCleanupController_CleanupOncePublishesCleanupTask(t *testing.T) {
	now := time.Now().UTC()
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-3": {NodeID: "node-3", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
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
		t.Fatalf("create plan: %v", err)
	}

	producer := &capturingTaskProducer{}
	controller := NewCleanupController(repo, CleanupControllerConfig{})
	controller.SetTaskProducer(producer)
	if err := controller.CleanupOnce(context.Background()); err != nil {
		t.Fatalf("cleanup once: %v", err)
	}
	if len(producer.cleanupTasks) != 1 {
		t.Fatalf("expected one cleanup task, got %d", len(producer.cleanupTasks))
	}
	if producer.cleanupTasks[0].PlanID != "rebalance-plan" || producer.cleanupTasks[0].NodeID != "node-1" {
		t.Fatalf("unexpected cleanup task %#v", producer.cleanupTasks[0])
	}
}

func TestRebalancePlanner_PlanOncePublishesRebalanceTask(t *testing.T) {
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
	}, "node-1", "node-2", "node-3")
	now := time.Now().UTC()
	for _, patch := range []store.NodeHeartbeatPatch{
		{NodeID: "node-1", Healthy: true, Capacity: 100, Used: 90, LastSeenAt: now},
		{NodeID: "node-2", Healthy: true, Capacity: 100, Used: 70, LastSeenAt: now},
		{NodeID: "node-3", Healthy: true, Capacity: 100, Used: 10, LastSeenAt: now},
	} {
		if err := repo.UpdateNodeHeartbeat(context.Background(), patch); err != nil {
			t.Fatalf("update node heartbeat: %v", err)
		}
	}
	desired := 2
	policy := metadata.ReplicaPolicy{DesiredReplicaCount: 2, MinimumReplicaCount: 1, CurrentReplicaCount: 2}
	if err := repo.UpdateChunkReplicas(context.Background(), store.ChunkReplicaPatch{
		Selector:      store.ChunkSelector{ID: "chunk-1"},
		ReplicaCount:  &desired,
		ReplicaPolicy: &policy,
		UpdatedAt:     now,
	}); err != nil {
		t.Fatalf("update chunk policy: %v", err)
	}

	producer := &capturingTaskProducer{}
	planner := NewRebalancePlanner(repo, RebalancePlannerConfig{HighWatermark: 0.85, LowWatermark: 0.60})
	planner.SetTaskProducer(producer)
	if err := planner.PlanOnce(context.Background()); err != nil {
		t.Fatalf("plan once: %v", err)
	}
	if len(producer.rebalanceTasks) != 1 {
		t.Fatalf("expected one rebalance task, got %d", len(producer.rebalanceTasks))
	}
	if producer.rebalanceTasks[0].TargetNodeID != "node-3" {
		t.Fatalf("unexpected rebalance task %#v", producer.rebalanceTasks[0])
	}
}

func TestFailoverPlanner_PlanOncePublishesFailoverTask(t *testing.T) {
	now := time.Now().UTC()
	staleSeen := now.Add(-10 * time.Minute)
	repo := newRepairFixtureRepository(t, metadata.ReplicaSet{
		"node-1": {NodeID: "node-1", Role: metadata.ReplicaRolePrimary, State: metadata.ReplicaStateReady, StoredSize: 16},
		"node-2": {NodeID: "node-2", Role: metadata.ReplicaRoleSecondary, State: metadata.ReplicaStateReady, StoredSize: 16},
	}, "node-1", "node-2", "node-3")
	if err := repo.UpdateNodeHeartbeat(context.Background(), store.NodeHeartbeatPatch{
		NodeID: "node-1", Healthy: true, Capacity: 1024, Used: 0, LastSeenAt: staleSeen,
	}); err != nil {
		t.Fatalf("mark node stale: %v", err)
	}

	producer := &capturingTaskProducer{}
	planner := NewFailoverPlanner(repo, FailoverPlannerConfig{NodeTimeout: 5 * time.Minute})
	planner.SetTaskProducer(producer)
	if err := planner.PlanOnce(context.Background()); err != nil {
		t.Fatalf("plan once: %v", err)
	}
	if len(producer.failoverTasks) != 1 {
		t.Fatalf("expected one failover task, got %d", len(producer.failoverTasks))
	}
	if producer.failoverTasks[0].TargetNodeID != "node-3" {
		t.Fatalf("unexpected failover task %#v", producer.failoverTasks[0])
	}
}

var _ mdsmq.TaskProducer = (*capturingTaskProducer)(nil)

type capturingTaskProducer struct {
	repairTasks    []contracts.ReplicaRepairTask
	cleanupTasks   []contracts.CleanupTask
	rebalanceTasks []contracts.RebalanceTask
	failoverTasks  []contracts.FailoverTask
}

func (c *capturingTaskProducer) PublishReplicaRepair(ctx context.Context, task contracts.ReplicaRepairTask) error {
	c.repairTasks = append(c.repairTasks, task)
	return nil
}

func (c *capturingTaskProducer) PublishCleanup(ctx context.Context, task contracts.CleanupTask) error {
	c.cleanupTasks = append(c.cleanupTasks, task)
	return nil
}

func (c *capturingTaskProducer) PublishRebalance(ctx context.Context, task contracts.RebalanceTask) error {
	c.rebalanceTasks = append(c.rebalanceTasks, task)
	return nil
}

func (c *capturingTaskProducer) PublishFailover(ctx context.Context, task contracts.FailoverTask) error {
	c.failoverTasks = append(c.failoverTasks, task)
	return nil
}
