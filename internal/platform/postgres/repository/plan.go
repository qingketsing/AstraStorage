package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5"
)

const replicaPlanColumns = `
id,
plan_type,
chunk_id,
file_id,
source_node_id,
target_node_id,
required_bytes,
state,
priority,
last_error_code,
last_error_message,
retry_count,
next_retry_at,
created_at,
updated_at,
completed_at
`

func (r *Repository) CreateReplicaPlan(ctx context.Context, plan *metadata.ReplicaPlan) error {
	return createReplicaPlan(ctx, r.pool, plan)
}

func (tx *Tx) CreateReplicaPlan(ctx context.Context, plan *metadata.ReplicaPlan) error {
	return createReplicaPlan(ctx, tx.tx, plan)
}

func (r *Repository) GetReplicaPlan(ctx context.Context, id string) (*metadata.ReplicaPlan, error) {
	return getReplicaPlan(ctx, r.pool, id)
}

func (tx *Tx) GetReplicaPlan(ctx context.Context, id string) (*metadata.ReplicaPlan, error) {
	return getReplicaPlan(ctx, tx.tx, id)
}

func (r *Repository) ListReplicaPlans(ctx context.Context, filter store.ReplicaPlanFilter) ([]metadata.ReplicaPlan, error) {
	return listReplicaPlans(ctx, r.pool, filter)
}

func (tx *Tx) ListReplicaPlans(ctx context.Context, filter store.ReplicaPlanFilter) ([]metadata.ReplicaPlan, error) {
	return listReplicaPlans(ctx, tx.tx, filter)
}

func (r *Repository) UpdateReplicaPlan(ctx context.Context, patch store.ReplicaPlanPatch) error {
	return updateReplicaPlan(ctx, r.pool, patch)
}

func (tx *Tx) UpdateReplicaPlan(ctx context.Context, patch store.ReplicaPlanPatch) error {
	return updateReplicaPlan(ctx, tx.tx, patch)
}

func (r *Repository) DeleteReplicaPlan(ctx context.Context, id string) error {
	return deleteReplicaPlan(ctx, r.pool, id)
}

func (tx *Tx) DeleteReplicaPlan(ctx context.Context, id string) error {
	return deleteReplicaPlan(ctx, tx.tx, id)
}

func createReplicaPlan(ctx context.Context, db queryDB, plan *metadata.ReplicaPlan) error {
	if plan == nil {
		return fmt.Errorf("%w: replica plan is nil", store.ErrInvalidArgument)
	}
	if strings.TrimSpace(plan.ID) == "" {
		return fmt.Errorf("%w: replica plan id is required", store.ErrInvalidArgument)
	}
	if plan.Type == "" || plan.ChunkID == "" || plan.FileID == "" || plan.SourceNodeID == "" || plan.TargetNodeID == "" || plan.State == "" {
		return fmt.Errorf("%w: replica plan fields are incomplete", store.ErrInvalidArgument)
	}
	if plan.SourceNodeID == plan.TargetNodeID {
		return fmt.Errorf("%w: source and target node ids must differ", store.ErrInvalidArgument)
	}
	if plan.RequiredBytes < 0 {
		return fmt.Errorf("%w: required bytes cannot be negative", store.ErrInvalidArgument)
	}

	var duplicateCount int64
	if err := db.QueryRow(ctx, `
SELECT COUNT(*)
FROM mds_replica_plans
WHERE chunk_id = $1
  AND plan_type = $2
  AND target_node_id = $3
  AND state NOT IN ('done', 'failed')
`, string(plan.ChunkID), string(plan.Type), string(plan.TargetNodeID)).Scan(&duplicateCount); err != nil {
		return fmt.Errorf("postgres repository: count active replica plans: %w", err)
	}
	if duplicateCount > 0 {
		return fmt.Errorf("%w: active replica plan for chunk %q target %q", store.ErrAlreadyExists, plan.ChunkID, plan.TargetNodeID)
	}

	_, err := db.Exec(ctx, `
INSERT INTO mds_replica_plans (
	id, plan_type, chunk_id, file_id, source_node_id, target_node_id, required_bytes, state, priority,
	last_error_code, last_error_message, retry_count, next_retry_at, created_at, updated_at, completed_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
`,
		plan.ID,
		string(plan.Type),
		string(plan.ChunkID),
		string(plan.FileID),
		string(plan.SourceNodeID),
		string(plan.TargetNodeID),
		plan.RequiredBytes,
		string(plan.State),
		plan.Priority,
		plan.LastErrorCode,
		plan.LastErrorMessage,
		plan.RetryCount,
		plan.NextRetryAt,
		plan.CreatedAt,
		plan.UpdatedAt,
		plan.CompletedAt,
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func getReplicaPlan(ctx context.Context, db queryDB, id string) (*metadata.ReplicaPlan, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: replica plan id is required", store.ErrInvalidArgument)
	}
	plan, err := scanReplicaPlan(db.QueryRow(ctx, "SELECT "+replicaPlanColumns+" FROM mds_replica_plans WHERE id = $1 LIMIT 1", id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: replica plan", store.ErrNotFound)
		}
		return nil, err
	}
	return plan, nil
}

func listReplicaPlans(ctx context.Context, db queryDB, filter store.ReplicaPlanFilter) ([]metadata.ReplicaPlan, error) {
	query := "SELECT " + replicaPlanColumns + " FROM mds_replica_plans"
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 6)
	add := func(clause string, value any) {
		clauses = append(clauses, fmt.Sprintf(clause, len(args)+1))
		args = append(args, value)
	}
	if len(filter.Types) > 0 {
		values := make([]string, 0, len(filter.Types))
		for _, item := range filter.Types {
			values = append(values, string(item))
		}
		add("plan_type = ANY($%d)", values)
	}
	if len(filter.States) > 0 {
		values := make([]string, 0, len(filter.States))
		for _, item := range filter.States {
			values = append(values, string(item))
		}
		add("state = ANY($%d)", values)
	}
	if filter.ChunkID != "" {
		add("chunk_id = $%d", string(filter.ChunkID))
	}
	if filter.FileID != "" {
		add("file_id = $%d", string(filter.FileID))
	}
	if filter.SourceNodeID != "" {
		add("source_node_id = $%d", string(filter.SourceNodeID))
	}
	if filter.TargetNodeID != "" {
		add("target_node_id = $%d", string(filter.TargetNodeID))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at, id"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres repository: list replica plans query: %w", err)
	}
	defer rows.Close()

	plans := make([]metadata.ReplicaPlan, 0)
	for rows.Next() {
		plan, err := scanReplicaPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, *plan)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate replica plans: %w", err)
	}
	return plans, nil
}

func updateReplicaPlan(ctx context.Context, db queryDB, patch store.ReplicaPlanPatch) error {
	if strings.TrimSpace(patch.ID) == "" {
		return fmt.Errorf("%w: replica plan id is required", store.ErrInvalidArgument)
	}
	var state any
	if patch.State != nil {
		state = string(*patch.State)
	}
	_, err := db.Exec(ctx, `
UPDATE mds_replica_plans
SET state = COALESCE($1, state),
    last_error_code = COALESCE($2, last_error_code),
    last_error_message = COALESCE($3, last_error_message),
    retry_count = COALESCE($4, retry_count),
    next_retry_at = COALESCE($5, next_retry_at),
    completed_at = COALESCE($6, completed_at),
    updated_at = $7
WHERE id = $8
`, state, patch.LastErrorCode, patch.LastErrorMessage, patch.RetryCount, patch.NextRetryAt, patch.CompletedAt, patch.UpdatedAt, patch.ID)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func deleteReplicaPlan(ctx context.Context, db queryDB, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: replica plan id is required", store.ErrInvalidArgument)
	}
	_, err := db.Exec(ctx, `DELETE FROM mds_replica_plans WHERE id = $1`, id)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func scanReplicaPlan(row rowScanner) (*metadata.ReplicaPlan, error) {
	var plan metadata.ReplicaPlan
	var planType string
	var state string
	var chunkID string
	var fileID string
	var sourceNodeID string
	var targetNodeID string
	var nextRetryAt sql.NullTime
	var completedAt sql.NullTime
	if err := row.Scan(
		&plan.ID,
		&planType,
		&chunkID,
		&fileID,
		&sourceNodeID,
		&targetNodeID,
		&plan.RequiredBytes,
		&state,
		&plan.Priority,
		&plan.LastErrorCode,
		&plan.LastErrorMessage,
		&plan.RetryCount,
		&nextRetryAt,
		&plan.CreatedAt,
		&plan.UpdatedAt,
		&completedAt,
	); err != nil {
		return nil, err
	}
	plan.Type = metadata.ReplicaPlanType(planType)
	plan.ChunkID = metadata.ChunkID(chunkID)
	plan.FileID = metadata.FileID(fileID)
	plan.SourceNodeID = metadata.NodeID(sourceNodeID)
	plan.TargetNodeID = metadata.NodeID(targetNodeID)
	plan.State = metadata.ReplicaPlanState(state)
	if nextRetryAt.Valid {
		t := nextRetryAt.Time
		plan.NextRetryAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		plan.CompletedAt = &t
	}
	return &plan, nil
}
