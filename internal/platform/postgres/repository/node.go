package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5"
)

const nodeColumns = `
id,
address,
rack,
zone,
region,
labels,
capacity,
used,
healthy,
last_seen_at,
updated_at
`

func (r *Repository) UpsertNode(ctx context.Context, node metadata.NodeInfo) error {
	return upsertNode(ctx, r.pool, node)
}

func (tx *Tx) UpsertNode(ctx context.Context, node metadata.NodeInfo) error {
	return upsertNode(ctx, tx.tx, node)
}

func (r *Repository) GetNode(ctx context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	return getNode(ctx, r.pool, nodeID)
}

func (tx *Tx) GetNode(ctx context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	return getNode(ctx, tx.tx, nodeID)
}

func (r *Repository) ListNodes(ctx context.Context, filter store.NodeFilter) ([]metadata.NodeInfo, error) {
	return listNodes(ctx, r.pool, filter)
}

func (tx *Tx) ListNodes(ctx context.Context, filter store.NodeFilter) ([]metadata.NodeInfo, error) {
	return listNodes(ctx, tx.tx, filter)
}

func (r *Repository) UpdateNodeHeartbeat(ctx context.Context, heartbeat store.NodeHeartbeatPatch) error {
	return updateNodeHeartbeat(ctx, r.pool, heartbeat)
}

func (tx *Tx) UpdateNodeHeartbeat(ctx context.Context, heartbeat store.NodeHeartbeatPatch) error {
	return updateNodeHeartbeat(ctx, tx.tx, heartbeat)
}

func upsertNode(ctx context.Context, db queryDB, node metadata.NodeInfo) error {
	if node.ID == "" {
		return fmt.Errorf("%w: node id is required", store.ErrInvalidArgument)
	}
	if node.Capacity < 0 || node.Used < 0 {
		return fmt.Errorf("%w: node capacity and used space cannot be negative", store.ErrInvalidArgument)
	}
	labels, err := marshalJSON(node.Labels, map[string]string{})
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
INSERT INTO mds_nodes (id, address, rack, zone, region, labels, capacity, used, healthy, last_seen_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11)
ON CONFLICT (id) DO UPDATE
SET address = EXCLUDED.address,
    rack = EXCLUDED.rack,
    zone = EXCLUDED.zone,
    region = EXCLUDED.region,
    labels = EXCLUDED.labels,
    capacity = EXCLUDED.capacity,
    used = EXCLUDED.used,
    healthy = EXCLUDED.healthy,
    last_seen_at = EXCLUDED.last_seen_at,
    updated_at = EXCLUDED.updated_at
`,
		string(node.ID),
		node.Address,
		node.Rack,
		node.Zone,
		node.Region,
		labels,
		node.Capacity,
		node.Used,
		node.Healthy,
		node.LastSeenAt,
		node.UpdatedAt,
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func getNode(ctx context.Context, db queryDB, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	node, err := scanNode(db.QueryRow(ctx, "SELECT "+nodeColumns+" FROM mds_nodes WHERE id = $1 LIMIT 1", string(nodeID)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: node", store.ErrNotFound)
		}
		return nil, err
	}
	return node, nil
}

func listNodes(ctx context.Context, db queryDB, filter store.NodeFilter) ([]metadata.NodeInfo, error) {
	where := make([]string, 0, 5)
	args := make([]any, 0, 5)
	add := func(clause string, value any) {
		where = append(where, fmt.Sprintf(clause, len(args)+1))
		args = append(args, value)
	}
	if len(filter.IDs) > 0 {
		ids := make([]string, 0, len(filter.IDs))
		for _, id := range filter.IDs {
			ids = append(ids, string(id))
		}
		add("id = ANY($%d)", ids)
	}
	if filter.HealthyOnly {
		add("healthy = $%d", true)
	}
	if filter.Zone != "" {
		add("zone = $%d", filter.Zone)
	}
	if filter.Rack != "" {
		add("rack = $%d", filter.Rack)
	}
	if len(filter.Labels) > 0 {
		labels, err := marshalJSON(filter.Labels, map[string]string{})
		if err != nil {
			return nil, err
		}
		where = append(where, fmt.Sprintf("labels @> $%d::jsonb", len(args)+1))
		args = append(args, labels)
	}
	query := "SELECT " + nodeColumns + " FROM mds_nodes"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY id"
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, filter.Offset)
	}
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres repository: list nodes query: %w", err)
	}
	defer rows.Close()
	nodes := make([]metadata.NodeInfo, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate nodes: %w", err)
	}
	return nodes, nil
}

func updateNodeHeartbeat(ctx context.Context, db queryDB, heartbeat store.NodeHeartbeatPatch) error {
	if heartbeat.Capacity < 0 || heartbeat.Used < 0 {
		return fmt.Errorf("%w: node capacity and used space cannot be negative", store.ErrInvalidArgument)
	}
	node, err := getNode(ctx, db, heartbeat.NodeID)
	if err != nil {
		return err
	}
	_ = node
	_, err = db.Exec(ctx, `
UPDATE mds_nodes
SET healthy = $1, capacity = $2, used = $3, last_seen_at = $4, updated_at = $4
WHERE id = $5
`, heartbeat.Healthy, heartbeat.Capacity, heartbeat.Used, heartbeat.LastSeenAt, string(heartbeat.NodeID))
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func scanNode(row rowScanner) (*metadata.NodeInfo, error) {
	var node metadata.NodeInfo
	var id string
	var labelsBytes []byte
	var lastSeenAt sql.NullTime
	if err := row.Scan(
		&id,
		&node.Address,
		&node.Rack,
		&node.Zone,
		&node.Region,
		&labelsBytes,
		&node.Capacity,
		&node.Used,
		&node.Healthy,
		&lastSeenAt,
		&node.UpdatedAt,
	); err != nil {
		return nil, err
	}
	node.ID = metadata.NodeID(id)
	if err := unmarshalJSON(labelsBytes, &node.Labels); err != nil {
		return nil, err
	}
	if lastSeenAt.Valid {
		t := lastSeenAt.Time
		node.LastSeenAt = &t
	}
	return &node, nil
}

func ensureNodesExist(ctx context.Context, db queryDB, nodeIDs []metadata.NodeID, updatedAt time.Time) error {
	for _, nodeID := range nodeIDs {
		if nodeID == "" {
			continue
		}
		if _, err := db.Exec(ctx, `
INSERT INTO mds_nodes (id, updated_at)
VALUES ($1, $2)
ON CONFLICT (id) DO NOTHING
`, string(nodeID), updatedAt); err != nil {
			return translateExecError(err)
		}
	}
	return nil
}
