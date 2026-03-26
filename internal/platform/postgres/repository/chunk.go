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

const chunkColumns = `
id,
file_id,
chunk_index,
chunk_offset,
size,
status,
version,
checksum_algorithm,
checksum_value,
checksum_verified,
checksum_verified_at,
desired_replica_count,
minimum_replica_count,
current_replica_count,
replica_count,
created_at,
updated_at,
verified_at,
last_error_code
`

func (r *Repository) UpsertChunks(ctx context.Context, chunks []metadata.ChunkMetadata) error {
	return upsertChunks(ctx, r.pool, chunks)
}

func (tx *Tx) UpsertChunks(ctx context.Context, chunks []metadata.ChunkMetadata) error {
	return upsertChunks(ctx, tx.tx, chunks)
}

func (r *Repository) GetChunk(ctx context.Context, selector store.ChunkSelector) (*metadata.ChunkMetadata, error) {
	return getChunk(ctx, r.pool, selector)
}

func (tx *Tx) GetChunk(ctx context.Context, selector store.ChunkSelector) (*metadata.ChunkMetadata, error) {
	return getChunk(ctx, tx.tx, selector)
}

func (r *Repository) ListChunksByFile(ctx context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	return listChunksByFile(ctx, r.pool, fileID)
}

func (tx *Tx) ListChunksByFile(ctx context.Context, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	return listChunksByFile(ctx, tx.tx, fileID)
}

func (r *Repository) ListChunksByNode(ctx context.Context, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error) {
	return listChunksByNode(ctx, r.pool, nodeID)
}

func (tx *Tx) ListChunksByNode(ctx context.Context, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error) {
	return listChunksByNode(ctx, tx.tx, nodeID)
}

func (r *Repository) UpdateChunkStatus(ctx context.Context, patch store.ChunkStatusPatch) error {
	return updateChunkStatus(ctx, r.pool, patch)
}

func (tx *Tx) UpdateChunkStatus(ctx context.Context, patch store.ChunkStatusPatch) error {
	return updateChunkStatus(ctx, tx.tx, patch)
}

func (r *Repository) UpdateChunkReplicas(ctx context.Context, patch store.ChunkReplicaPatch) error {
	return updateChunkReplicas(ctx, r.pool, patch)
}

func (tx *Tx) UpdateChunkReplicas(ctx context.Context, patch store.ChunkReplicaPatch) error {
	return updateChunkReplicas(ctx, tx.tx, patch)
}

func (r *Repository) DeleteChunk(ctx context.Context, selector store.ChunkSelector) error {
	return deleteChunk(ctx, r.pool, selector)
}

func (tx *Tx) DeleteChunk(ctx context.Context, selector store.ChunkSelector) error {
	return deleteChunk(ctx, tx.tx, selector)
}

func (r *Repository) RemoveChunkReplica(ctx context.Context, selector store.ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error {
	return removeChunkReplica(ctx, r.pool, selector, nodeID, updatedAt)
}

func (tx *Tx) RemoveChunkReplica(ctx context.Context, selector store.ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error {
	return removeChunkReplica(ctx, tx.tx, selector, nodeID, updatedAt)
}

func upsertChunks(ctx context.Context, db queryDB, chunks []metadata.ChunkMetadata) error {
	pending := make(map[metadata.ChunkID]metadata.ChunkMetadata, len(chunks))
	for _, chunk := range chunks {
		if chunk.ID == "" || chunk.FileID == "" {
			return fmt.Errorf("%w: chunk id and file id are required", store.ErrInvalidArgument)
		}
		if _, err := getFile(ctx, db, store.FileSelector{ID: chunk.FileID}); err != nil {
			return err
		}
		if chunk.Size < 0 {
			return fmt.Errorf("%w: chunk size cannot be negative", store.ErrInvalidArgument)
		}
		if chunk.Size > metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: chunk size cannot exceed %d", store.ErrInvalidArgument, metadata.FixedChunkSizeBytes)
		}
		if chunk.Offset != chunk.Index*metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: chunk offset must equal index * chunk size", store.ErrInvalidArgument)
		}
		for _, existing := range pending {
			if existing.FileID != chunk.FileID || existing.ID == chunk.ID {
				continue
			}
			if existing.Index == chunk.Index {
				return fmt.Errorf("%w: duplicate chunk index %d for file %q", store.ErrAlreadyExists, chunk.Index, chunk.FileID)
			}
			if existing.Offset == chunk.Offset {
				return fmt.Errorf("%w: duplicate chunk offset %d for file %q", store.ErrAlreadyExists, chunk.Offset, chunk.FileID)
			}
		}
		if err := ensureChunkUniqueness(ctx, db, chunk); err != nil {
			return err
		}
		pending[chunk.ID] = chunk
	}

	for _, chunk := range pending {
		_, err := db.Exec(ctx, `
INSERT INTO mds_chunks (
	id, file_id, chunk_index, chunk_offset, size, status, version, checksum_algorithm, checksum_value, checksum_verified,
	checksum_verified_at, desired_replica_count, minimum_replica_count, current_replica_count, replica_count, created_at, updated_at, verified_at, last_error_code
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
ON CONFLICT (id) DO UPDATE
SET file_id = EXCLUDED.file_id,
    chunk_index = EXCLUDED.chunk_index,
    chunk_offset = EXCLUDED.chunk_offset,
    size = EXCLUDED.size,
    status = EXCLUDED.status,
    version = EXCLUDED.version,
    checksum_algorithm = EXCLUDED.checksum_algorithm,
    checksum_value = EXCLUDED.checksum_value,
    checksum_verified = EXCLUDED.checksum_verified,
    checksum_verified_at = EXCLUDED.checksum_verified_at,
    desired_replica_count = EXCLUDED.desired_replica_count,
    minimum_replica_count = EXCLUDED.minimum_replica_count,
    current_replica_count = EXCLUDED.current_replica_count,
    replica_count = EXCLUDED.replica_count,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at,
    verified_at = EXCLUDED.verified_at,
    last_error_code = EXCLUDED.last_error_code
`,
			string(chunk.ID),
			string(chunk.FileID),
			chunk.Index,
			chunk.Offset,
			chunk.Size,
			string(chunk.Status),
			chunk.Version,
			chunk.Checksum.Algorithm,
			chunk.Checksum.Value,
			chunk.Checksum.Verified,
			chunk.Checksum.VerifiedAt,
			chunk.ReplicaPolicy.DesiredReplicaCount,
			chunk.ReplicaPolicy.MinimumReplicaCount,
			chunk.ReplicaPolicy.CurrentReplicaCount,
			chunk.ReplicaCount,
			chunk.CreatedAt,
			chunk.UpdatedAt,
			chunk.VerifiedAt,
			chunk.LastErrorCode,
		)
		if err != nil {
			return translateExecError(err)
		}
		if len(chunk.Replicas) > 0 {
			count := chunk.ReplicaCount
			if count == 0 {
				count = len(chunk.Replicas)
			}
			if err := updateChunkReplicas(ctx, db, store.ChunkReplicaPatch{
				Selector:     store.ChunkSelector{ID: chunk.ID},
				Upserts:      chunk.Replicas,
				ReplicaCount: &count,
				ReplicaPolicy: &metadata.ReplicaPolicy{
					DesiredReplicaCount: chunk.ReplicaPolicy.DesiredReplicaCount,
					MinimumReplicaCount: chunk.ReplicaPolicy.MinimumReplicaCount,
					CurrentReplicaCount: chunk.ReplicaPolicy.CurrentReplicaCount,
				},
				UpdatedAt: chunk.UpdatedAt,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func getChunk(ctx context.Context, db queryDB, selector store.ChunkSelector) (*metadata.ChunkMetadata, error) {
	where, args, err := buildChunkSelectorWhere(selector)
	if err != nil {
		return nil, err
	}
	chunk, err := scanChunk(db.QueryRow(ctx, "SELECT "+chunkColumns+" FROM mds_chunks WHERE "+where+" LIMIT 1", args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: chunk", store.ErrNotFound)
		}
		return nil, err
	}
	if err := loadChunkReplicas(ctx, db, map[metadata.ChunkID]*metadata.ChunkMetadata{chunk.ID: chunk}); err != nil {
		return nil, err
	}
	return chunk, nil
}

func listChunksByFile(ctx context.Context, db queryDB, fileID metadata.FileID) ([]metadata.ChunkMetadata, error) {
	rows, err := db.Query(ctx, "SELECT "+chunkColumns+" FROM mds_chunks WHERE file_id = $1 ORDER BY chunk_index", string(fileID))
	if err != nil {
		return nil, fmt.Errorf("postgres repository: list chunks query: %w", err)
	}
	defer rows.Close()

	chunks := make([]metadata.ChunkMetadata, 0)
	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, *chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate chunks: %w", err)
	}
	chunkMap := make(map[metadata.ChunkID]*metadata.ChunkMetadata, len(chunks))
	for i := range chunks {
		chunkMap[chunks[i].ID] = &chunks[i]
	}
	if err := loadChunkReplicas(ctx, db, chunkMap); err != nil {
		return nil, err
	}
	return chunks, nil
}

func listChunksByNode(ctx context.Context, db queryDB, nodeID metadata.NodeID) ([]metadata.ChunkMetadata, error) {
	rows, err := db.Query(ctx, `
SELECT DISTINCT `+chunkColumns+`
FROM mds_chunks c
JOIN mds_chunk_replicas r ON r.chunk_id = c.id
WHERE r.node_id = $1
ORDER BY c.file_id, c.chunk_index
`, string(nodeID))
	if err != nil {
		return nil, fmt.Errorf("postgres repository: list chunks by node query: %w", err)
	}
	defer rows.Close()

	chunks := make([]metadata.ChunkMetadata, 0)
	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, *chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate chunks by node: %w", err)
	}
	chunkMap := make(map[metadata.ChunkID]*metadata.ChunkMetadata, len(chunks))
	for i := range chunks {
		chunkMap[chunks[i].ID] = &chunks[i]
	}
	if err := loadChunkReplicas(ctx, db, chunkMap); err != nil {
		return nil, err
	}
	return chunks, nil
}

func updateChunkStatus(ctx context.Context, db queryDB, patch store.ChunkStatusPatch) error {
	chunk, err := getChunk(ctx, db, patch.Selector)
	if err != nil {
		return err
	}
	checksum := chunk.Checksum
	if patch.Checksum != nil {
		checksum = *patch.Checksum
	}
	verifiedAt := chunk.VerifiedAt
	if patch.VerifiedAt != nil {
		verifiedAt = patch.VerifiedAt
	}
	_, err = db.Exec(ctx, `
UPDATE mds_chunks
SET status = $1,
    checksum_algorithm = $2,
    checksum_value = $3,
    checksum_verified = $4,
    checksum_verified_at = $5,
    last_error_code = $6,
    verified_at = $7,
    updated_at = $8
WHERE id = $9
`,
		string(patch.Status),
		checksum.Algorithm,
		checksum.Value,
		checksum.Verified,
		checksum.VerifiedAt,
		patch.LastErrorCode,
		verifiedAt,
		patch.UpdatedAt,
		string(chunk.ID),
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func updateChunkReplicas(ctx context.Context, db queryDB, patch store.ChunkReplicaPatch) error {
	chunk, err := getChunk(ctx, db, patch.Selector)
	if err != nil {
		return err
	}
	nodeIDs := make([]metadata.NodeID, 0, len(patch.Upserts))
	for nodeID := range patch.Upserts {
		nodeIDs = append(nodeIDs, nodeID)
	}
	if err := ensureNodesExist(ctx, db, nodeIDs, patch.UpdatedAt); err != nil {
		return err
	}
	for nodeID, replica := range patch.Upserts {
		_, err := db.Exec(ctx, `
INSERT INTO mds_chunk_replicas (
	id, file_id, chunk_id, node_id, role, state, checksum_algorithm, checksum_value, checksum_verified, checksum_verified_at,
	stored_size, created_at, updated_at, verified_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (chunk_id, node_id) DO UPDATE
SET id = EXCLUDED.id,
    file_id = EXCLUDED.file_id,
    role = EXCLUDED.role,
    state = EXCLUDED.state,
    checksum_algorithm = EXCLUDED.checksum_algorithm,
    checksum_value = EXCLUDED.checksum_value,
    checksum_verified = EXCLUDED.checksum_verified,
    checksum_verified_at = EXCLUDED.checksum_verified_at,
    stored_size = EXCLUDED.stored_size,
    created_at = EXCLUDED.created_at,
    updated_at = EXCLUDED.updated_at,
    verified_at = EXCLUDED.verified_at
`,
			replica.ID,
			string(replica.FileID),
			string(chunk.ID),
			string(nodeID),
			string(replica.Role),
			string(replica.State),
			replica.Checksum.Algorithm,
			replica.Checksum.Value,
			replica.Checksum.Verified,
			replica.Checksum.VerifiedAt,
			replica.StoredSize,
			replica.CreatedAt,
			replica.UpdatedAt,
			replica.VerifiedAt,
		)
		if err != nil {
			return translateExecError(err)
		}
	}
	for _, nodeID := range patch.RemoveNodeIDs {
		if _, err := db.Exec(ctx, `DELETE FROM mds_chunk_replicas WHERE chunk_id = $1 AND node_id = $2`, string(chunk.ID), string(nodeID)); err != nil {
			return translateExecError(err)
		}
	}

	replicaCount := 0
	if patch.ReplicaCount != nil {
		replicaCount = *patch.ReplicaCount
	} else {
		if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM mds_chunk_replicas WHERE chunk_id = $1`, string(chunk.ID)).Scan(&replicaCount); err != nil {
			return fmt.Errorf("postgres repository: count chunk replicas: %w", err)
		}
	}
	replicaPolicy := chunk.ReplicaPolicy
	if patch.ReplicaPolicy != nil {
		replicaPolicy = *patch.ReplicaPolicy
	}
	_, err = db.Exec(ctx, `
UPDATE mds_chunks
SET replica_count = $1,
    desired_replica_count = $2,
    minimum_replica_count = $3,
    current_replica_count = $4,
    updated_at = $5
WHERE id = $6
`,
		replicaCount,
		replicaPolicy.DesiredReplicaCount,
		replicaPolicy.MinimumReplicaCount,
		replicaPolicy.CurrentReplicaCount,
		patch.UpdatedAt,
		string(chunk.ID),
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func deleteChunk(ctx context.Context, db queryDB, selector store.ChunkSelector) error {
	chunk, err := getChunk(ctx, db, selector)
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `DELETE FROM mds_chunks WHERE id = $1`, string(chunk.ID)); err != nil {
		return translateExecError(err)
	}
	return nil
}

func removeChunkReplica(ctx context.Context, db queryDB, selector store.ChunkSelector, nodeID metadata.NodeID, updatedAt time.Time) error {
	chunk, err := getChunk(ctx, db, selector)
	if err != nil {
		return err
	}
	replica, ok := chunk.Replicas[nodeID]
	if !ok {
		return fmt.Errorf("%w: replica on node %q", store.ErrNotFound, nodeID)
	}
	_ = replica
	if _, err := db.Exec(ctx, `DELETE FROM mds_chunk_replicas WHERE chunk_id = $1 AND node_id = $2`, string(chunk.ID), string(nodeID)); err != nil {
		return translateExecError(err)
	}
	var replicaCount int
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM mds_chunk_replicas WHERE chunk_id = $1`, string(chunk.ID)).Scan(&replicaCount); err != nil {
		return fmt.Errorf("postgres repository: count chunk replicas: %w", err)
	}
	_, err = db.Exec(ctx, `
UPDATE mds_chunks
SET replica_count = $1,
    current_replica_count = $2,
    updated_at = $3
WHERE id = $4
`, replicaCount, replicaCount, updatedAt, string(chunk.ID))
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func buildChunkSelectorWhere(selector store.ChunkSelector) (string, []any, error) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 3)
	add := func(column string, value any) {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, value)
	}
	if selector.ID != "" {
		add("id", string(selector.ID))
	}
	if selector.FileID != "" {
		add("file_id", string(selector.FileID))
	}
	if selector.Index != nil {
		add("chunk_index", *selector.Index)
	}
	if len(clauses) == 0 {
		return "", nil, fmt.Errorf("%w: chunk selector is empty", store.ErrInvalidArgument)
	}
	return strings.Join(clauses, " AND "), args, nil
}

func scanChunk(row rowScanner) (*metadata.ChunkMetadata, error) {
	var chunk metadata.ChunkMetadata
	var id string
	var fileID string
	var status string
	var checksumVerifiedAt sql.NullTime
	var verifiedAt sql.NullTime
	if err := row.Scan(
		&id,
		&fileID,
		&chunk.Index,
		&chunk.Offset,
		&chunk.Size,
		&status,
		&chunk.Version,
		&chunk.Checksum.Algorithm,
		&chunk.Checksum.Value,
		&chunk.Checksum.Verified,
		&checksumVerifiedAt,
		&chunk.ReplicaPolicy.DesiredReplicaCount,
		&chunk.ReplicaPolicy.MinimumReplicaCount,
		&chunk.ReplicaPolicy.CurrentReplicaCount,
		&chunk.ReplicaCount,
		&chunk.CreatedAt,
		&chunk.UpdatedAt,
		&verifiedAt,
		&chunk.LastErrorCode,
	); err != nil {
		return nil, err
	}
	chunk.ID = metadata.ChunkID(id)
	chunk.FileID = metadata.FileID(fileID)
	chunk.Status = metadata.ChunkStatus(status)
	if checksumVerifiedAt.Valid {
		t := checksumVerifiedAt.Time
		chunk.Checksum.VerifiedAt = &t
	}
	if verifiedAt.Valid {
		t := verifiedAt.Time
		chunk.VerifiedAt = &t
	}
	return &chunk, nil
}

func loadChunkReplicas(ctx context.Context, db queryDB, chunks map[metadata.ChunkID]*metadata.ChunkMetadata) error {
	if len(chunks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(chunks))
	for chunkID := range chunks {
		ids = append(ids, string(chunkID))
	}
	rows, err := db.Query(ctx, `
SELECT chunk_id, node_id, id, file_id, role, state, checksum_algorithm, checksum_value, checksum_verified, checksum_verified_at, stored_size, created_at, updated_at, verified_at
FROM mds_chunk_replicas
WHERE chunk_id = ANY($1)
ORDER BY chunk_id, node_id
`, ids)
	if err != nil {
		return fmt.Errorf("postgres repository: query chunk replicas: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var chunkID string
		var nodeID string
		var replica metadata.ReplicaMetadata
		var role string
		var state string
		var fileID string
		var checksumVerifiedAt sql.NullTime
		var verifiedAt sql.NullTime
		if err := rows.Scan(
			&chunkID,
			&nodeID,
			&replica.ID,
			&fileID,
			&role,
			&state,
			&replica.Checksum.Algorithm,
			&replica.Checksum.Value,
			&replica.Checksum.Verified,
			&checksumVerifiedAt,
			&replica.StoredSize,
			&replica.CreatedAt,
			&replica.UpdatedAt,
			&verifiedAt,
		); err != nil {
			return fmt.Errorf("postgres repository: scan chunk replica: %w", err)
		}
		replica.FileID = metadata.FileID(fileID)
		replica.ChunkID = metadata.ChunkID(chunkID)
		replica.NodeID = metadata.NodeID(nodeID)
		replica.Role = metadata.ReplicaRole(role)
		replica.State = metadata.ReplicaState(state)
		if checksumVerifiedAt.Valid {
			t := checksumVerifiedAt.Time
			replica.Checksum.VerifiedAt = &t
		}
		if verifiedAt.Valid {
			t := verifiedAt.Time
			replica.VerifiedAt = &t
		}
		chunk := chunks[metadata.ChunkID(chunkID)]
		if chunk == nil {
			continue
		}
		if chunk.Replicas == nil {
			chunk.Replicas = make(metadata.ReplicaSet)
		}
		chunk.Replicas[metadata.NodeID(nodeID)] = replica
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres repository: iterate chunk replicas: %w", err)
	}
	return nil
}

func ensureChunkUniqueness(ctx context.Context, db queryDB, chunk metadata.ChunkMetadata) error {
	var existingID string
	err := db.QueryRow(ctx, `
SELECT id
FROM mds_chunks
WHERE file_id = $1 AND chunk_index = $2 AND id <> $3
LIMIT 1
`, string(chunk.FileID), chunk.Index, string(chunk.ID)).Scan(&existingID)
	switch {
	case err == nil:
		return fmt.Errorf("%w: duplicate chunk index %d for file %q", store.ErrAlreadyExists, chunk.Index, chunk.FileID)
	case !errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("postgres repository: check chunk index uniqueness: %w", err)
	}

	err = db.QueryRow(ctx, `
SELECT id
FROM mds_chunks
WHERE file_id = $1 AND chunk_offset = $2 AND id <> $3
LIMIT 1
`, string(chunk.FileID), chunk.Offset, string(chunk.ID)).Scan(&existingID)
	switch {
	case err == nil:
		return fmt.Errorf("%w: duplicate chunk offset %d for file %q", store.ErrAlreadyExists, chunk.Offset, chunk.FileID)
	case errors.Is(err, pgx.ErrNoRows):
		return nil
	default:
		return fmt.Errorf("postgres repository: check chunk offset uniqueness: %w", err)
	}
}
