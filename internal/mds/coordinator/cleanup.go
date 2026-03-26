package coordinator

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"AstraStorage/internal/mds/metadata"
	mdsmq "AstraStorage/internal/mds/mq"
	"AstraStorage/internal/mds/store"
	"AstraStorage/internal/platform/mq/contracts"
)

type CleanupControllerConfig struct {
	Interval       time.Duration
	HTTPTimeout    time.Duration
	RetryBackoff   time.Duration
	MaxPlansPerRun int
}

type CleanupController struct {
	repo           store.Repository
	httpClient     *http.Client
	interval       time.Duration
	retryBackoff   time.Duration
	maxPlansPerRun int
	taskProducer   mdsmq.TaskProducer
}

func NewCleanupController(repo store.Repository, cfg CleanupControllerConfig) *CleanupController {
	return newCleanupController(repo, cfg, &http.Client{Timeout: cfg.HTTPTimeout})
}

func newCleanupController(repo store.Repository, cfg CleanupControllerConfig, httpClient *http.Client) *CleanupController {
	if cfg.RetryBackoff <= 0 {
		cfg.RetryBackoff = time.Minute
	}
	if cfg.MaxPlansPerRun <= 0 {
		cfg.MaxPlansPerRun = 32
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.HTTPTimeout}
	}
	if httpClient.Timeout <= 0 {
		httpClient.Timeout = 5 * time.Second
	}
	return &CleanupController{
		repo:           repo,
		httpClient:     httpClient,
		interval:       cfg.Interval,
		retryBackoff:   cfg.RetryBackoff,
		maxPlansPerRun: cfg.MaxPlansPerRun,
	}
}

func (c *CleanupController) SetTaskProducer(producer mdsmq.TaskProducer) {
	if c == nil {
		return
	}
	c.taskProducer = producer
}

func (c *CleanupController) ExecuteCleanup(ctx context.Context, task contracts.CleanupTask) error {
	if c == nil || c.repo == nil {
		return fmt.Errorf("cleanup controller: repository is nil")
	}
	plan, err := c.repo.GetReplicaPlan(ctx, task.PlanID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	switch plan.Type {
	case metadata.ReplicaPlanTypeFailover:
		return c.purgeLostReplicaMetadata(ctx, *plan, now)
	case metadata.ReplicaPlanTypeRebalance:
		if err := c.deleteSourceReplica(ctx, *plan, now); err != nil {
			return c.failOrRetryPlan(ctx, *plan, err, now)
		}
		return nil
	default:
		return fmt.Errorf("cleanup controller: unsupported replica plan type %q", plan.Type)
	}
}

func (c *CleanupController) Run(ctx context.Context) {
	if c == nil || c.interval <= 0 {
		return
	}
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.CleanupOnce(ctx)
		}
	}
}

func (c *CleanupController) CleanupOnce(ctx context.Context) error {
	if c == nil || c.repo == nil {
		return nil
	}
	now := time.Now().UTC()
	plans, err := c.repo.ListReplicaPlans(ctx, store.ReplicaPlanFilter{
		Types: []metadata.ReplicaPlanType{
			metadata.ReplicaPlanTypeFailover,
			metadata.ReplicaPlanTypeRebalance,
		},
		States: []metadata.ReplicaPlanState{
			metadata.ReplicaPlanStateMaterialized,
			metadata.ReplicaPlanStateCopyReady,
			metadata.ReplicaPlanStateCleanupReady,
		},
		Limit: c.maxPlansPerRun,
	})
	if err != nil {
		return err
	}
	for _, plan := range plans {
		if err := c.finalizeCompletedPlan(ctx, plan, now); err != nil {
			return err
		}
	}
	return nil
}

func (c *CleanupController) finalizeCompletedPlan(ctx context.Context, plan metadata.ReplicaPlan, now time.Time) error {
	chunk, err := c.repo.GetChunk(ctx, store.ChunkSelector{ID: plan.ChunkID})
	if err != nil {
		return err
	}
	targetReplica, ok := chunk.Replicas[plan.TargetNodeID]
	if !ok || targetReplica.State != metadata.ReplicaStateReady {
		return nil
	}
	if c.taskProducer != nil {
		if plan.State == metadata.ReplicaPlanStateCleanupReady {
			return nil
		}
		if err := c.taskProducer.PublishCleanup(ctx, mdsmq.NewCleanupTask(plan)); err != nil {
			return c.failOrRetryPlan(ctx, plan, err, now)
		}
		state := metadata.ReplicaPlanStateCleanupReady
		return c.repo.UpdateReplicaPlan(ctx, store.ReplicaPlanPatch{
			ID:        plan.ID,
			State:     &state,
			UpdatedAt: now,
		})
	}

	switch plan.Type {
	case metadata.ReplicaPlanTypeFailover:
		return c.purgeLostReplicaMetadata(ctx, plan, now)
	case metadata.ReplicaPlanTypeRebalance:
		if err := c.deleteSourceReplica(ctx, plan, now); err != nil {
			return c.failOrRetryPlan(ctx, plan, err, now)
		}
		return nil
	default:
		return nil
	}
}

func (c *CleanupController) deleteSourceReplica(ctx context.Context, plan metadata.ReplicaPlan, now time.Time) error {
	sourceNode, err := c.repo.GetNode(ctx, plan.SourceNodeID)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, sourceNode.Address+"/chunks/"+string(plan.ChunkID), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cleanup delete source replica returned status %d", resp.StatusCode)
	}

	return c.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.RemoveChunkReplica(ctx, store.ChunkSelector{ID: plan.ChunkID}, plan.SourceNodeID, now); err != nil {
			return err
		}
		return tx.UpdateReplicaPlan(ctx, store.ReplicaPlanPatch{
			ID:          plan.ID,
			State:       replicaPlanStatePtr(metadata.ReplicaPlanStateDone),
			CompletedAt: &now,
			UpdatedAt:   now,
		})
	})
}

func (c *CleanupController) purgeLostReplicaMetadata(ctx context.Context, plan metadata.ReplicaPlan, now time.Time) error {
	return c.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		if err := tx.RemoveChunkReplica(ctx, store.ChunkSelector{ID: plan.ChunkID}, plan.SourceNodeID, now); err != nil {
			return err
		}
		return tx.UpdateReplicaPlan(ctx, store.ReplicaPlanPatch{
			ID:          plan.ID,
			State:       replicaPlanStatePtr(metadata.ReplicaPlanStateDone),
			CompletedAt: &now,
			UpdatedAt:   now,
		})
	})
}

func (c *CleanupController) failOrRetryPlan(ctx context.Context, plan metadata.ReplicaPlan, err error, now time.Time) error {
	retryCount := plan.RetryCount + 1
	nextRetryAt := now.Add(c.retryBackoff)
	lastErrorCode := "cleanup_failed"
	lastErrorMessage := err.Error()
	return c.repo.UpdateReplicaPlan(ctx, store.ReplicaPlanPatch{
		ID:               plan.ID,
		RetryCount:       &retryCount,
		NextRetryAt:      &nextRetryAt,
		LastErrorCode:    &lastErrorCode,
		LastErrorMessage: &lastErrorMessage,
		UpdatedAt:        now,
	})
}

func replicaPlanStatePtr(state metadata.ReplicaPlanState) *metadata.ReplicaPlanState {
	return &state
}
