package store

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"AstraStorage/internal/mds/metadata"
)

func (r *memoryRepository) CreateReplicaPlan(_ context.Context, plan *metadata.ReplicaPlan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return createReplicaPlan(&r.state, plan)
}

func (tx *memoryTx) CreateReplicaPlan(_ context.Context, plan *metadata.ReplicaPlan) error {
	return createReplicaPlan(&tx.state, plan)
}

func createReplicaPlan(state *memoryState, plan *metadata.ReplicaPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: replica plan is nil", ErrInvalidArgument)
	}
	if strings.TrimSpace(plan.ID) == "" {
		return fmt.Errorf("%w: replica plan id is required", ErrInvalidArgument)
	}
	if plan.Type == "" {
		return fmt.Errorf("%w: replica plan type is required", ErrInvalidArgument)
	}
	if plan.ChunkID == "" || plan.FileID == "" {
		return fmt.Errorf("%w: replica plan chunk id and file id are required", ErrInvalidArgument)
	}
	if plan.SourceNodeID == "" || plan.TargetNodeID == "" {
		return fmt.Errorf("%w: replica plan source and target node ids are required", ErrInvalidArgument)
	}
	if plan.SourceNodeID == plan.TargetNodeID {
		return fmt.Errorf("%w: source and target node ids must differ", ErrInvalidArgument)
	}
	if plan.RequiredBytes < 0 {
		return fmt.Errorf("%w: required bytes cannot be negative", ErrInvalidArgument)
	}
	if plan.State == "" {
		return fmt.Errorf("%w: replica plan state is required", ErrInvalidArgument)
	}
	if _, exists := state.replicaPlans[plan.ID]; exists {
		return fmt.Errorf("%w: replica plan %q", ErrAlreadyExists, plan.ID)
	}
	for _, existing := range state.replicaPlans {
		if existing.Type != plan.Type || existing.ChunkID != plan.ChunkID || existing.TargetNodeID != plan.TargetNodeID {
			continue
		}
		if isTerminalReplicaPlanState(existing.State) {
			continue
		}
		return fmt.Errorf("%w: active replica plan for chunk %q target %q", ErrAlreadyExists, plan.ChunkID, plan.TargetNodeID)
	}
	state.replicaPlans[plan.ID] = cloneReplicaPlan(plan)
	return nil
}

func (r *memoryRepository) GetReplicaPlan(_ context.Context, id string) (*metadata.ReplicaPlan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return getReplicaPlan(r.state, id)
}

func (tx *memoryTx) GetReplicaPlan(_ context.Context, id string) (*metadata.ReplicaPlan, error) {
	return getReplicaPlan(tx.state, id)
}

func getReplicaPlan(state memoryState, id string) (*metadata.ReplicaPlan, error) {
	plan, ok := state.replicaPlans[id]
	if !ok {
		return nil, fmt.Errorf("%w: replica plan", ErrNotFound)
	}
	return cloneReplicaPlan(plan), nil
}

func (r *memoryRepository) ListReplicaPlans(_ context.Context, filter ReplicaPlanFilter) ([]metadata.ReplicaPlan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listReplicaPlans(r.state, filter), nil
}

func (tx *memoryTx) ListReplicaPlans(_ context.Context, filter ReplicaPlanFilter) ([]metadata.ReplicaPlan, error) {
	return listReplicaPlans(tx.state, filter), nil
}

func listReplicaPlans(state memoryState, filter ReplicaPlanFilter) []metadata.ReplicaPlan {
	plans := make([]metadata.ReplicaPlan, 0)
	for _, plan := range state.replicaPlans {
		if len(filter.Types) > 0 && !slices.Contains(filter.Types, plan.Type) {
			continue
		}
		if len(filter.States) > 0 && !slices.Contains(filter.States, plan.State) {
			continue
		}
		if filter.ChunkID != "" && plan.ChunkID != filter.ChunkID {
			continue
		}
		if filter.FileID != "" && plan.FileID != filter.FileID {
			continue
		}
		if filter.SourceNodeID != "" && plan.SourceNodeID != filter.SourceNodeID {
			continue
		}
		if filter.TargetNodeID != "" && plan.TargetNodeID != filter.TargetNodeID {
			continue
		}
		plans = append(plans, *cloneReplicaPlan(plan))
	}
	sort.Slice(plans, func(i, j int) bool {
		if !plans[i].CreatedAt.Equal(plans[j].CreatedAt) {
			return plans[i].CreatedAt.Before(plans[j].CreatedAt)
		}
		return plans[i].ID < plans[j].ID
	})
	return applyListWindow(plans, filter.Limit, filter.Offset)
}

func (r *memoryRepository) UpdateReplicaPlan(_ context.Context, patch ReplicaPlanPatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateReplicaPlan(&r.state, patch)
}

func (tx *memoryTx) UpdateReplicaPlan(_ context.Context, patch ReplicaPlanPatch) error {
	return updateReplicaPlan(&tx.state, patch)
}

func updateReplicaPlan(state *memoryState, patch ReplicaPlanPatch) error {
	plan, ok := state.replicaPlans[patch.ID]
	if !ok {
		return fmt.Errorf("%w: replica plan", ErrNotFound)
	}
	if patch.State != nil {
		plan.State = *patch.State
	}
	if patch.LastErrorCode != nil {
		plan.LastErrorCode = *patch.LastErrorCode
	}
	if patch.LastErrorMessage != nil {
		plan.LastErrorMessage = *patch.LastErrorMessage
	}
	if patch.RetryCount != nil {
		plan.RetryCount = *patch.RetryCount
	}
	if patch.NextRetryAt != nil {
		t := *patch.NextRetryAt
		plan.NextRetryAt = &t
	}
	if patch.CompletedAt != nil {
		t := *patch.CompletedAt
		plan.CompletedAt = &t
	}
	plan.UpdatedAt = patch.UpdatedAt
	return nil
}

func (r *memoryRepository) DeleteReplicaPlan(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return deleteReplicaPlan(&r.state, id)
}

func (tx *memoryTx) DeleteReplicaPlan(_ context.Context, id string) error {
	return deleteReplicaPlan(&tx.state, id)
}

func deleteReplicaPlan(state *memoryState, id string) error {
	if _, ok := state.replicaPlans[id]; !ok {
		return fmt.Errorf("%w: replica plan", ErrNotFound)
	}
	delete(state.replicaPlans, id)
	return nil
}

func isTerminalReplicaPlanState(state metadata.ReplicaPlanState) bool {
	return state == metadata.ReplicaPlanStateDone || state == metadata.ReplicaPlanStateFailed
}
