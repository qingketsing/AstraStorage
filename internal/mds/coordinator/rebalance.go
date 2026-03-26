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

type RebalancePlannerConfig struct {
	Interval       time.Duration
	HighWatermark  float64
	LowWatermark   float64
	MaxPlansPerRun int
}

type RebalancePlanner struct {
	repo           store.Repository
	interval       time.Duration
	highWatermark  float64
	lowWatermark   float64
	maxPlansPerRun int
	taskProducer   mdsmq.TaskProducer
}

type RebalanceMove struct {
	Chunk  metadata.ChunkMetadata
	Source metadata.NodeInfo
	Target metadata.NodeInfo
}

func NewRebalancePlanner(repo store.Repository, cfg RebalancePlannerConfig) *RebalancePlanner {
	if cfg.HighWatermark <= 0 {
		cfg.HighWatermark = 0.85
	}
	if cfg.LowWatermark <= 0 {
		cfg.LowWatermark = 0.60
	}
	if cfg.MaxPlansPerRun <= 0 {
		cfg.MaxPlansPerRun = 32
	}
	return &RebalancePlanner{
		repo:           repo,
		interval:       cfg.Interval,
		highWatermark:  cfg.HighWatermark,
		lowWatermark:   cfg.LowWatermark,
		maxPlansPerRun: cfg.MaxPlansPerRun,
	}
}

func (r *RebalancePlanner) SetTaskProducer(producer mdsmq.TaskProducer) {
	if r == nil {
		return
	}
	r.taskProducer = producer
}

func (r *RebalancePlanner) Run(ctx context.Context) {
	if r == nil || r.interval <= 0 {
		return
	}
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = r.PlanOnce(ctx)
		}
	}
}

func (r *RebalancePlanner) PlanOnce(ctx context.Context) error {
	if r == nil || r.repo == nil {
		return nil
	}
	now := time.Now().UTC()
	nodes, err := r.repo.ListNodes(ctx, store.NodeFilter{HealthyOnly: true})
	if err != nil {
		return err
	}
	overfull, underfull := r.classifyNodePressure(nodes)
	remaining := r.maxPlansPerRun
	for _, source := range overfull {
		if remaining == 0 {
			break
		}
		move, err := r.selectReplicaToMove(ctx, source, underfull)
		if err != nil {
			return err
		}
		if move == nil {
			continue
		}
		if err := r.planReplicaMove(ctx, *move, now); err != nil {
			return err
		}
		remaining--
	}
	return nil
}

func (r *RebalancePlanner) classifyNodePressure(nodes []metadata.NodeInfo) (overfull []metadata.NodeInfo, underfull []metadata.NodeInfo) {
	for _, node := range nodes {
		ratio := nodeUsageRatio(node)
		switch {
		case ratio >= r.highWatermark:
			overfull = append(overfull, node)
		case ratio <= r.lowWatermark:
			underfull = append(underfull, node)
		}
	}
	return overfull, underfull
}

func (r *RebalancePlanner) selectReplicaToMove(ctx context.Context, source metadata.NodeInfo, targets []metadata.NodeInfo) (*RebalanceMove, error) {
	chunks, err := r.repo.ListChunksByNode(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	allNodes, err := r.repo.ListNodes(ctx, store.NodeFilter{})
	if err != nil {
		return nil, err
	}
	nodeIndex := make(map[metadata.NodeID]metadata.NodeInfo, len(allNodes))
	for _, node := range allNodes {
		nodeIndex[node.ID] = node
	}

	for _, chunk := range chunks {
		activePlans, err := r.repo.ListReplicaPlans(ctx, store.ReplicaPlanFilter{
			Types: []metadata.ReplicaPlanType{
				metadata.ReplicaPlanTypeFailover,
				metadata.ReplicaPlanTypeRebalance,
			},
			States:  activeReplicaPlanStates(),
			ChunkID: chunk.ID,
		})
		if err != nil {
			return nil, err
		}
		if len(activePlans) > 0 {
			continue
		}

		desiredReplicaCount := chunk.ReplicaPolicy.DesiredReplicaCount
		if desiredReplicaCount <= 0 {
			desiredReplicaCount = metadata.DefaultReplicaCount
		}
		if rootmds.CountEffectiveReadyReplicas(chunk, nodeIndex) < desiredReplicaCount {
			continue
		}

		selected := rootmds.SelectPlacementTargets(rootmds.PlacementRequest{
			Candidates:    targets,
			Excluded:      rootmds.BuildReplicaExclusionSet(chunk),
			RequiredBytes: rootmds.RequiredPlacementBytes(chunk),
			Count:         1,
		})
		if len(selected) == 0 {
			continue
		}
		return &RebalanceMove{
			Chunk:  chunk,
			Source: source,
			Target: selected[0],
		}, nil
	}
	return nil, nil
}

func (r *RebalancePlanner) planReplicaMove(ctx context.Context, move RebalanceMove, now time.Time) error {
	plan := metadata.ReplicaPlan{
		ID:            fmt.Sprintf("rebalance-%s-%s-%d", move.Chunk.ID, move.Target.ID, now.UnixNano()),
		Type:          metadata.ReplicaPlanTypeRebalance,
		ChunkID:       move.Chunk.ID,
		FileID:        move.Chunk.FileID,
		SourceNodeID:  move.Source.ID,
		TargetNodeID:  move.Target.ID,
		RequiredBytes: rootmds.RequiredPlacementBytes(move.Chunk),
		State:         metadata.ReplicaPlanStateMaterialized,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := r.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.CreateReplicaPlan(ctx, &plan); err != nil {
			return err
		}
		replicaCount := len(move.Chunk.Replicas) + 1
		replicaPolicy := move.Chunk.ReplicaPolicy
		if replicaPolicy.DesiredReplicaCount <= 0 {
			replicaPolicy.DesiredReplicaCount = metadata.DefaultReplicaCount
		}
		replicaPolicy.CurrentReplicaCount = replicaCount
		return tx.UpdateChunkReplicas(ctx, store.ChunkReplicaPatch{
			Selector: store.ChunkSelector{ID: move.Chunk.ID},
			Upserts: metadata.ReplicaSet{
				move.Target.ID: {
					ID:         fmt.Sprintf("%s-%s", move.Chunk.ID, move.Target.ID),
					FileID:     move.Chunk.FileID,
					ChunkID:    move.Chunk.ID,
					NodeID:     move.Target.ID,
					Role:       metadata.ReplicaRoleSecondary,
					State:      metadata.ReplicaStatePending,
					StoredSize: 0,
					CreatedAt:  now,
					UpdatedAt:  now,
				},
			},
			ReplicaCount:  &replicaCount,
			ReplicaPolicy: &replicaPolicy,
			UpdatedAt:     now,
		})
	}); err != nil {
		return err
	}
	if r.taskProducer == nil {
		return nil
	}
	if err := r.taskProducer.PublishRebalance(ctx, mdsmq.NewRebalanceTask(plan)); err != nil {
		return r.rollbackPlannedReplica(ctx, plan.ID, move.Chunk, move.Target.ID, now)
	}
	return nil
}

func (r *RebalancePlanner) rollbackPlannedReplica(ctx context.Context, planID string, chunk metadata.ChunkMetadata, targetNodeID metadata.NodeID, now time.Time) error {
	return r.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
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

func nodeUsageRatio(node metadata.NodeInfo) float64 {
	if node.Capacity <= 0 {
		return 1
	}
	return float64(node.Used) / float64(node.Capacity)
}
