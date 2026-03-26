package coordinator

import (
	"context"
	"fmt"
	"time"

	rootmds "AstraStorage/internal/mds"
	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	"AstraStorage/internal/mds/store"
)

type FailoverPlannerConfig struct {
	Interval       time.Duration
	NodeTimeout    time.Duration
	MaxPlansPerRun int
}

type FailoverPlanner struct {
	repo           store.Repository
	interval       time.Duration
	nodeTimeout    time.Duration
	maxPlansPerRun int
	taskProducer   mdsmq.TaskProducer
}

func NewFailoverPlanner(repo store.Repository, cfg FailoverPlannerConfig) *FailoverPlanner {
	if cfg.MaxPlansPerRun <= 0 {
		cfg.MaxPlansPerRun = 32
	}
	return &FailoverPlanner{
		repo:           repo,
		interval:       cfg.Interval,
		nodeTimeout:    cfg.NodeTimeout,
		maxPlansPerRun: cfg.MaxPlansPerRun,
	}
}

func (f *FailoverPlanner) SetTaskProducer(producer mdsmq.TaskProducer) {
	if f == nil {
		return
	}
	f.taskProducer = producer
}

func (f *FailoverPlanner) Run(ctx context.Context) {
	if f == nil || f.interval <= 0 {
		return
	}
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = f.PlanOnce(ctx)
		}
	}
}

func (f *FailoverPlanner) PlanOnce(ctx context.Context) error {
	if f == nil || f.repo == nil {
		return nil
	}
	now := time.Now().UTC()
	unavailableNodes, err := f.listUnavailableNodes(ctx, now)
	if err != nil {
		return err
	}

	remaining := f.maxPlansPerRun
	for _, node := range unavailableNodes {
		if remaining == 0 {
			break
		}
		planned, err := f.planNodeFailover(ctx, node, now, remaining)
		remaining -= planned
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *FailoverPlanner) listUnavailableNodes(ctx context.Context, now time.Time) ([]metadata.NodeInfo, error) {
	nodes, err := f.repo.ListNodes(ctx, store.NodeFilter{})
	if err != nil {
		return nil, err
	}
	unavailable := make([]metadata.NodeInfo, 0)
	for _, node := range nodes {
		if f.isNodeUnavailable(node, now) {
			unavailable = append(unavailable, node)
		}
	}
	return unavailable, nil
}

func (f *FailoverPlanner) isNodeUnavailable(node metadata.NodeInfo, now time.Time) bool {
	if !node.Healthy {
		return true
	}
	if f.nodeTimeout <= 0 {
		return false
	}
	if node.LastSeenAt == nil {
		return true
	}
	return now.Sub(*node.LastSeenAt) > f.nodeTimeout
}

func (f *FailoverPlanner) planNodeFailover(ctx context.Context, node metadata.NodeInfo, now time.Time, remaining int) (int, error) {
	chunks, err := f.repo.ListChunksByNode(ctx, node.ID)
	if err != nil {
		return 0, err
	}
	planned := 0
	for _, chunk := range chunks {
		if remaining == 0 {
			break
		}
		ok, err := f.planChunkFailover(ctx, chunk, node.ID, now)
		if err != nil {
			return planned, err
		}
		if ok {
			planned++
			remaining--
		}
	}
	return planned, nil
}

func (f *FailoverPlanner) planChunkFailover(ctx context.Context, chunk metadata.ChunkMetadata, failedNodeID metadata.NodeID, now time.Time) (bool, error) {
	file, err := f.repo.GetFile(ctx, store.FileSelector{ID: chunk.FileID})
	if err != nil {
		return false, err
	}
	nodes, err := f.repo.ListNodes(ctx, store.NodeFilter{})
	if err != nil {
		return false, err
	}
	nodeIndex := make(map[metadata.NodeID]metadata.NodeInfo, len(nodes))
	for _, node := range nodes {
		nodeIndex[node.ID] = node
	}
	desiredReplicaCount := chunk.ReplicaPolicy.DesiredReplicaCount
	if desiredReplicaCount <= 0 {
		desiredReplicaCount = file.ReplicaPolicy.DesiredReplicaCount
	}
	if desiredReplicaCount <= 0 {
		desiredReplicaCount = metadata.DefaultReplicaCount
	}
	if rootmds.CountEffectiveReadyReplicas(chunk, nodeIndex) >= desiredReplicaCount {
		return false, nil
	}
	activePlans, err := f.repo.ListReplicaPlans(ctx, store.ReplicaPlanFilter{
		Types:   []metadata.ReplicaPlanType{metadata.ReplicaPlanTypeFailover},
		States:  activeReplicaPlanStates(),
		ChunkID: chunk.ID,
	})
	if err != nil {
		return false, err
	}
	if len(activePlans) > 0 {
		return false, nil
	}

	excluded := rootmds.BuildReplicaExclusionSet(chunk)
	selected := rootmds.SelectPlacementTargets(rootmds.PlacementRequest{
		Candidates:    nodes,
		Excluded:      excluded,
		RequiredBytes: rootmds.RequiredPlacementBytes(chunk),
		Count:         1,
	})
	if len(selected) == 0 {
		return false, nil
	}

	plan := metadata.ReplicaPlan{
		ID:            fmt.Sprintf("failover-%s-%s-%d", chunk.ID, selected[0].ID, now.UnixNano()),
		Type:          metadata.ReplicaPlanTypeFailover,
		ChunkID:       chunk.ID,
		FileID:        chunk.FileID,
		SourceNodeID:  failedNodeID,
		TargetNodeID:  selected[0].ID,
		RequiredBytes: rootmds.RequiredPlacementBytes(chunk),
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := f.materializePendingReplica(ctx, plan, selected[0], chunk, now); err != nil {
		return false, err
	}
	if f.taskProducer != nil {
		if err := f.taskProducer.PublishFailover(ctx, mdsmq.NewFailoverTask(plan)); err != nil {
			if rollbackErr := f.rollbackPendingReplica(ctx, plan.ID, chunk, selected[0].ID, now); rollbackErr != nil {
				return false, fmt.Errorf("publish failover task: %w (rollback: %v)", err, rollbackErr)
			}
			return false, err
		}
	}
	return true, nil
}

func (f *FailoverPlanner) materializePendingReplica(ctx context.Context, plan metadata.ReplicaPlan, target metadata.NodeInfo, chunk metadata.ChunkMetadata, now time.Time) error {
	return f.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateReplicaPlan(ctx, &plan); err != nil {
			return err
		}
		role := metadata.ReplicaRoleSecondary
		if len(chunk.Replicas) == 0 {
			role = metadata.ReplicaRolePrimary
		}
		replica := metadata.ReplicaMetadata{
			ID:         fmt.Sprintf("%s-%s", chunk.ID, target.ID),
			FileID:     chunk.FileID,
			ChunkID:    chunk.ID,
			NodeID:     target.ID,
			Role:       role,
			State:      metadata.ReplicaStatePending,
			StoredSize: 0,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		replicaCount := len(chunk.Replicas) + 1
		replicaPolicy := chunk.ReplicaPolicy
		if replicaPolicy.DesiredReplicaCount <= 0 {
			replicaPolicy.DesiredReplicaCount = metadata.DefaultReplicaCount
		}
		replicaPolicy.CurrentReplicaCount = replicaCount
		return tx.UpdateChunkReplicas(ctx, store.ChunkReplicaPatch{
			Selector:      store.ChunkSelector{ID: chunk.ID},
			Upserts:       metadata.ReplicaSet{target.ID: replica},
			ReplicaCount:  &replicaCount,
			ReplicaPolicy: &replicaPolicy,
			UpdatedAt:     now,
		})
	})
}

func (f *FailoverPlanner) rollbackPendingReplica(ctx context.Context, planID string, chunk metadata.ChunkMetadata, targetNodeID metadata.NodeID, now time.Time) error {
	return f.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		originalReplicaCount := len(chunk.Replicas)
		replicaPolicy := chunk.ReplicaPolicy
		replicaPolicy.CurrentReplicaCount = originalReplicaCount
		if err := tx.UpdateChunkReplicas(ctx, store.ChunkReplicaPatch{
			Selector:      store.ChunkSelector{ID: chunk.ID},
			RemoveNodeIDs: []metadata.NodeID{targetNodeID},
			ReplicaCount:  &originalReplicaCount,
			ReplicaPolicy: &replicaPolicy,
			UpdatedAt:     now,
		}); err != nil {
			return err
		}
		return tx.DeleteReplicaPlan(ctx, planID)
	})
}

func activeReplicaPlanStates() []metadata.ReplicaPlanState {
	return []metadata.ReplicaPlanState{
		metadata.ReplicaPlanStatePlanned,
		metadata.ReplicaPlanStateMaterialized,
		metadata.ReplicaPlanStateCopyReady,
		metadata.ReplicaPlanStateCleanupReady,
	}
}
