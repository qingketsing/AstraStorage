package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5"
)

const fileColumns = `
id,
namespace,
inode_id,
parent_inode_id,
path,
name,
size,
stored_size,
chunk_size,
version,
status,
content_type,
storage_class,
primary_node_id,
secondary_node_ids,
latest_upload_session_id,
checksum_algorithm,
checksum_value,
checksum_verified,
checksum_verified_at,
desired_replica_count,
minimum_replica_count,
current_replica_count,
user_metadata,
tags,
created_at,
updated_at,
completed_at
`

func (r *Repository) CreateFile(ctx context.Context, file *metadata.FileMetadata) error {
	return createFile(ctx, r.pool, file)
}

func (tx *Tx) CreateFile(ctx context.Context, file *metadata.FileMetadata) error {
	return createFile(ctx, tx.tx, file)
}

func (r *Repository) GetFile(ctx context.Context, selector store.FileSelector) (*metadata.FileMetadata, error) {
	return getFile(ctx, r.pool, selector)
}

func (tx *Tx) GetFile(ctx context.Context, selector store.FileSelector) (*metadata.FileMetadata, error) {
	return getFile(ctx, tx.tx, selector)
}

func (r *Repository) ListFiles(ctx context.Context, filter store.FileFilter) ([]*metadata.FileMetadata, error) {
	return listFiles(ctx, r.pool, filter)
}

func (tx *Tx) ListFiles(ctx context.Context, filter store.FileFilter) ([]*metadata.FileMetadata, error) {
	return listFiles(ctx, tx.tx, filter)
}

func (r *Repository) UpdateFile(ctx context.Context, patch store.FilePatch) error {
	return updateFile(ctx, r.pool, patch)
}

func (tx *Tx) UpdateFile(ctx context.Context, patch store.FilePatch) error {
	return updateFile(ctx, tx.tx, patch)
}

func (r *Repository) UpdateFilePlacements(ctx context.Context, patch store.FilePlacementPatch) error {
	return updateFilePlacements(ctx, r.pool, patch)
}

func (tx *Tx) UpdateFilePlacements(ctx context.Context, patch store.FilePlacementPatch) error {
	return updateFilePlacements(ctx, tx.tx, patch)
}

func (r *Repository) DeleteFile(ctx context.Context, selector store.FileSelector) error {
	return deleteFile(ctx, r.pool, selector)
}

func (tx *Tx) DeleteFile(ctx context.Context, selector store.FileSelector) error {
	return deleteFile(ctx, tx.tx, selector)
}

func createFile(ctx context.Context, db queryDB, file *metadata.FileMetadata) error {
	if file == nil {
		return fmt.Errorf("%w: file is nil", store.ErrInvalidArgument)
	}
	if file.ID == "" || file.InodeID == "" {
		return fmt.Errorf("%w: file id and inode id are required", store.ErrInvalidArgument)
	}
	if file.ChunkSize == 0 {
		file.ChunkSize = metadata.FixedChunkSizeBytes
	}
	if file.ChunkSize != metadata.FixedChunkSizeBytes {
		return fmt.Errorf("%w: chunk size must be %d", store.ErrInvalidArgument, metadata.FixedChunkSizeBytes)
	}

	inode, err := getInode(ctx, db, store.InodeSelector{ID: file.InodeID})
	if err != nil {
		return err
	}
	if inode.Type != metadata.InodeTypeFile {
		return fmt.Errorf("%w: inode %q is not a file", store.ErrInvalidArgument, inode.ID)
	}
	if existing, err := getFile(ctx, db, store.FileSelector{InodeID: file.InodeID}); err == nil && existing != nil {
		return fmt.Errorf("%w: inode %q already has file %q", store.ErrAlreadyExists, file.InodeID, existing.ID)
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	secondaryNodeIDs, err := marshalJSON(file.SecondaryNodeIDs, []metadata.NodeID{})
	if err != nil {
		return err
	}
	userMetadata, err := marshalJSON(file.UserMetadata, map[string]string{})
	if err != nil {
		return err
	}
	tags, err := marshalJSON(file.Tags, map[string]string{})
	if err != nil {
		return err
	}

	_, err = db.Exec(ctx, `
INSERT INTO mds_files (
	id, namespace, inode_id, parent_inode_id, path, name, size, stored_size, chunk_size, version, status, content_type,
	storage_class, primary_node_id, secondary_node_ids, latest_upload_session_id, checksum_algorithm, checksum_value,
	checksum_verified, checksum_verified_at, desired_replica_count, minimum_replica_count, current_replica_count,
	user_metadata, tags, created_at, updated_at, completed_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16, $17, $18, $19, $20, $21, $22, $23, $24::jsonb, $25::jsonb, $26, $27, $28)
`,
		string(file.ID),
		file.Namespace,
		string(file.InodeID),
		string(file.ParentInodeID),
		file.Path,
		file.Name,
		file.Size,
		file.StoredSize,
		file.ChunkSize,
		file.Version,
		string(file.Status),
		file.ContentType,
		file.StorageClass,
		string(file.PrimaryNodeID),
		secondaryNodeIDs,
		string(file.LatestUploadSessionID),
		file.Checksum.Algorithm,
		file.Checksum.Value,
		file.Checksum.Verified,
		file.Checksum.VerifiedAt,
		file.ReplicaPolicy.DesiredReplicaCount,
		file.ReplicaPolicy.MinimumReplicaCount,
		file.ReplicaPolicy.CurrentReplicaCount,
		userMetadata,
		tags,
		file.CreatedAt,
		file.UpdatedAt,
		file.CompletedAt,
	)
	if err != nil {
		return translateExecError(err)
	}

	if len(file.NodePlacements) > 0 {
		if err := updateFilePlacements(ctx, db, store.FilePlacementPatch{
			Selector:  store.FileSelector{ID: file.ID},
			Upserts:   file.NodePlacements,
			UpdatedAt: file.UpdatedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func getFile(ctx context.Context, db queryDB, selector store.FileSelector) (*metadata.FileMetadata, error) {
	where, args, err := buildFileSelectorWhere(selector)
	if err != nil {
		return nil, err
	}

	query := "SELECT " + fileColumns + " FROM mds_files WHERE " + where + " LIMIT 1"
	file, err := scanFile(db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: file", store.ErrNotFound)
		}
		return nil, err
	}

	if err := loadFilePlacements(ctx, db, map[metadata.FileID]*metadata.FileMetadata{file.ID: file}); err != nil {
		return nil, err
	}
	return file, nil
}

func listFiles(ctx context.Context, db queryDB, filter store.FileFilter) ([]*metadata.FileMetadata, error) {
	where := make([]string, 0, 5)
	args := make([]any, 0, 5)
	add := func(clause string, value any) {
		where = append(where, fmt.Sprintf(clause, len(args)+1))
		args = append(args, value)
	}

	if filter.Namespace != "" {
		add("namespace = $%d", filter.Namespace)
	}
	if filter.ParentInodeID != "" {
		add("parent_inode_id = $%d", string(filter.ParentInodeID))
	}
	if filter.PathPrefix != "" {
		add("path LIKE $%d", filter.PathPrefix+"%")
	}
	if len(filter.Status) > 0 {
		statuses := make([]string, 0, len(filter.Status))
		for _, status := range filter.Status {
			statuses = append(statuses, string(status))
		}
		add("status = ANY($%d)", statuses)
	}
	if filter.NodeID != "" {
		add(`(
primary_node_id = $%d OR
EXISTS (SELECT 1 FROM jsonb_array_elements_text(secondary_node_ids) AS sid(value) WHERE sid.value = $%1$d) OR
EXISTS (SELECT 1 FROM mds_file_placements AS placement WHERE placement.file_id = mds_files.id AND placement.node_id = $%1$d)
)`, string(filter.NodeID))
	}

	query := "SELECT " + fileColumns + " FROM mds_files"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY path"
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
		return nil, fmt.Errorf("postgres repository: list files query: %w", err)
	}
	defer rows.Close()

	files := make([]*metadata.FileMetadata, 0)
	fileMap := make(map[metadata.FileID]*metadata.FileMetadata)
	for rows.Next() {
		file, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
		fileMap[file.ID] = file
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate files: %w", err)
	}
	if err := loadFilePlacements(ctx, db, fileMap); err != nil {
		return nil, err
	}
	return files, nil
}

func updateFile(ctx context.Context, db queryDB, patch store.FilePatch) error {
	file, err := getFile(ctx, db, patch.Selector)
	if err != nil {
		return err
	}

	clauses := make([]string, 0, 15)
	args := make([]any, 0, 18)
	add := func(column string, value any) {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, value)
	}

	if patch.ParentInodeID != nil {
		add("parent_inode_id", string(*patch.ParentInodeID))
	}
	if patch.Path != nil {
		add("path", *patch.Path)
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Size != nil {
		add("size", *patch.Size)
	}
	if patch.StoredSize != nil {
		add("stored_size", *patch.StoredSize)
	}
	if patch.ChunkSize != nil {
		if *patch.ChunkSize != metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: chunk size must be %d", store.ErrInvalidArgument, metadata.FixedChunkSizeBytes)
		}
		add("chunk_size", *patch.ChunkSize)
	}
	if patch.Version != nil {
		add("version", *patch.Version)
	}
	if patch.Status != nil {
		add("status", string(*patch.Status))
	}
	if patch.PrimaryNodeID != nil {
		add("primary_node_id", string(*patch.PrimaryNodeID))
	}
	if patch.SecondaryNodeIDs != nil {
		value, err := marshalJSON(patch.SecondaryNodeIDs, []metadata.NodeID{})
		if err != nil {
			return err
		}
		clauses = append(clauses, fmt.Sprintf("secondary_node_ids = $%d::jsonb", len(args)+1))
		args = append(args, value)
	}
	if patch.LatestUploadSessionID != nil {
		add("latest_upload_session_id", string(*patch.LatestUploadSessionID))
	}
	if patch.Checksum != nil {
		add("checksum_algorithm", patch.Checksum.Algorithm)
		add("checksum_value", patch.Checksum.Value)
		add("checksum_verified", patch.Checksum.Verified)
		add("checksum_verified_at", patch.Checksum.VerifiedAt)
	}
	if patch.ReplicaPolicy != nil {
		add("desired_replica_count", patch.ReplicaPolicy.DesiredReplicaCount)
		add("minimum_replica_count", patch.ReplicaPolicy.MinimumReplicaCount)
		add("current_replica_count", patch.ReplicaPolicy.CurrentReplicaCount)
	}
	if patch.UserMetadata != nil {
		value, err := marshalJSON(patch.UserMetadata, map[string]string{})
		if err != nil {
			return err
		}
		clauses = append(clauses, fmt.Sprintf("user_metadata = $%d::jsonb", len(args)+1))
		args = append(args, value)
	}
	if patch.Tags != nil {
		value, err := marshalJSON(patch.Tags, map[string]string{})
		if err != nil {
			return err
		}
		clauses = append(clauses, fmt.Sprintf("tags = $%d::jsonb", len(args)+1))
		args = append(args, value)
	}
	if patch.CompletedAt != nil {
		add("completed_at", *patch.CompletedAt)
	}
	add("updated_at", patch.UpdatedAt)

	args = append(args, string(file.ID))
	query := "UPDATE mds_files SET " + strings.Join(clauses, ", ") + fmt.Sprintf(" WHERE id = $%d", len(args))
	if _, err := db.Exec(ctx, query, args...); err != nil {
		return translateExecError(err)
	}
	return nil
}

func updateFilePlacements(ctx context.Context, db queryDB, patch store.FilePlacementPatch) error {
	file, err := getFile(ctx, db, patch.Selector)
	if err != nil {
		return err
	}
	if len(patch.ExpectedStatus) > 0 && !containsFileStatus(patch.ExpectedStatus, file.Status) {
		return fmt.Errorf("%w: file status mismatch", store.ErrConflict)
	}
	nodeIDs := make([]metadata.NodeID, 0, len(patch.Upserts))
	for nodeID := range patch.Upserts {
		nodeIDs = append(nodeIDs, nodeID)
	}
	if err := ensureNodesExist(ctx, db, nodeIDs, patch.UpdatedAt); err != nil {
		return err
	}

	for nodeID, placement := range patch.Upserts {
		chunkIDs, err := marshalJSON(placement.ChunkIDs, []metadata.ChunkID{})
		if err != nil {
			return err
		}
		_, err = db.Exec(ctx, `
INSERT INTO mds_file_placements (
	file_id, node_id, replica_role, replica_state, is_primary, chunk_ids, stored_size, checksum_state, last_sync_at
)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)
ON CONFLICT (file_id, node_id) DO UPDATE
SET replica_role = EXCLUDED.replica_role,
    replica_state = EXCLUDED.replica_state,
    is_primary = EXCLUDED.is_primary,
    chunk_ids = EXCLUDED.chunk_ids,
    stored_size = EXCLUDED.stored_size,
    checksum_state = EXCLUDED.checksum_state,
    last_sync_at = EXCLUDED.last_sync_at
`,
			string(file.ID),
			string(nodeID),
			string(placement.ReplicaRole),
			string(placement.ReplicaState),
			placement.IsPrimary,
			chunkIDs,
			placement.StoredSize,
			placement.ChecksumState,
			placement.LastSyncAt,
		)
		if err != nil {
			return translateExecError(err)
		}
	}
	for _, nodeID := range patch.RemoveNodeIDs {
		if _, err := db.Exec(ctx, `DELETE FROM mds_file_placements WHERE file_id = $1 AND node_id = $2`, string(file.ID), string(nodeID)); err != nil {
			return translateExecError(err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE mds_files SET updated_at = $1 WHERE id = $2`, patch.UpdatedAt, string(file.ID)); err != nil {
		return translateExecError(err)
	}
	return nil
}

func deleteFile(ctx context.Context, db queryDB, selector store.FileSelector) error {
	file, err := getFile(ctx, db, selector)
	if err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `DELETE FROM mds_files WHERE id = $1`, string(file.ID)); err != nil {
		return translateExecError(err)
	}
	return nil
}

func buildFileSelectorWhere(selector store.FileSelector) (string, []any, error) {
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 6)
	add := func(column string, value any) {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, value)
	}

	if selector.ID != "" {
		add("id", string(selector.ID))
	}
	if selector.InodeID != "" {
		add("inode_id", string(selector.InodeID))
	}
	if selector.ParentInodeID != "" {
		add("parent_inode_id", string(selector.ParentInodeID))
	}
	if selector.Namespace != "" {
		add("namespace", selector.Namespace)
	}
	if selector.Path != "" {
		add("path", selector.Path)
	}
	if selector.Name != "" {
		add("name", selector.Name)
	}
	if selector.Version != nil {
		add("version", *selector.Version)
	}
	if len(clauses) == 0 {
		return "", nil, fmt.Errorf("%w: file selector is empty", store.ErrInvalidArgument)
	}
	return strings.Join(clauses, " AND "), args, nil
}

func scanFile(row rowScanner) (*metadata.FileMetadata, error) {
	var file metadata.FileMetadata
	var id string
	var inodeID string
	var parentInodeID string
	var status string
	var primaryNodeID string
	var latestUploadSessionID string
	var secondaryNodeIDsBytes []byte
	var checksumVerifiedAt sql.NullTime
	var userMetadataBytes []byte
	var tagsBytes []byte
	var completedAt sql.NullTime

	if err := row.Scan(
		&id,
		&file.Namespace,
		&inodeID,
		&parentInodeID,
		&file.Path,
		&file.Name,
		&file.Size,
		&file.StoredSize,
		&file.ChunkSize,
		&file.Version,
		&status,
		&file.ContentType,
		&file.StorageClass,
		&primaryNodeID,
		&secondaryNodeIDsBytes,
		&latestUploadSessionID,
		&file.Checksum.Algorithm,
		&file.Checksum.Value,
		&file.Checksum.Verified,
		&checksumVerifiedAt,
		&file.ReplicaPolicy.DesiredReplicaCount,
		&file.ReplicaPolicy.MinimumReplicaCount,
		&file.ReplicaPolicy.CurrentReplicaCount,
		&userMetadataBytes,
		&tagsBytes,
		&file.CreatedAt,
		&file.UpdatedAt,
		&completedAt,
	); err != nil {
		return nil, err
	}

	file.ID = metadata.FileID(id)
	file.InodeID = metadata.InodeID(inodeID)
	file.ParentInodeID = metadata.InodeID(parentInodeID)
	file.Status = metadata.FileStatus(status)
	file.PrimaryNodeID = metadata.NodeID(primaryNodeID)
	file.LatestUploadSessionID = metadata.UploadSessionID(latestUploadSessionID)
	if checksumVerifiedAt.Valid {
		t := checksumVerifiedAt.Time
		file.Checksum.VerifiedAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		file.CompletedAt = &t
	}
	if err := unmarshalJSON(secondaryNodeIDsBytes, &file.SecondaryNodeIDs); err != nil {
		return nil, err
	}
	if err := unmarshalJSON(userMetadataBytes, &file.UserMetadata); err != nil {
		return nil, err
	}
	if err := unmarshalJSON(tagsBytes, &file.Tags); err != nil {
		return nil, err
	}
	return &file, nil
}

func loadFilePlacements(ctx context.Context, db queryDB, files map[metadata.FileID]*metadata.FileMetadata) error {
	if len(files) == 0 {
		return nil
	}

	ids := make([]string, 0, len(files))
	for fileID := range files {
		ids = append(ids, string(fileID))
	}

	rows, err := db.Query(ctx, `
SELECT
	placement.file_id,
	placement.node_id,
	placement.replica_role,
	placement.replica_state,
	placement.is_primary,
	placement.chunk_ids,
	placement.stored_size,
	placement.checksum_state,
	placement.last_sync_at,
	node.id,
	node.address,
	node.rack,
	node.zone,
	node.region,
	node.labels,
	node.capacity,
	node.used,
	node.healthy,
	node.last_seen_at,
	node.updated_at
FROM mds_file_placements AS placement
JOIN mds_nodes AS node ON node.id = placement.node_id
WHERE placement.file_id = ANY($1)
ORDER BY placement.file_id, placement.node_id
`, ids)
	if err != nil {
		return fmt.Errorf("postgres repository: query file placements: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fileID string
		var nodeID string
		var role string
		var state string
		var chunkIDsBytes []byte
		var lastSyncAt sql.NullTime
		var node metadata.NodeInfo
		var nodeLabelsBytes []byte
		var lastSeenAt sql.NullTime
		var updatedAt sql.NullTime
		var nodeIDEcho string
		var isPrimary bool
		var storedSize int64
		var checksumState string

		if err := rows.Scan(
			&fileID,
			&nodeID,
			&role,
			&state,
			&isPrimary,
			&chunkIDsBytes,
			&storedSize,
			&checksumState,
			&lastSyncAt,
			&nodeIDEcho,
			&node.Address,
			&node.Rack,
			&node.Zone,
			&node.Region,
			&nodeLabelsBytes,
			&node.Capacity,
			&node.Used,
			&node.Healthy,
			&lastSeenAt,
			&updatedAt,
		); err != nil {
			return fmt.Errorf("postgres repository: scan file placement: %w", err)
		}

		file := files[metadata.FileID(fileID)]
		if file == nil {
			continue
		}
		var placement metadata.NodePlacement

		node.ID = metadata.NodeID(nodeIDEcho)
		if err := unmarshalJSON(nodeLabelsBytes, &node.Labels); err != nil {
			return err
		}
		if lastSeenAt.Valid {
			t := lastSeenAt.Time
			node.LastSeenAt = &t
		}
		if updatedAt.Valid {
			node.UpdatedAt = updatedAt.Time
		}
		placement.Node = node
		placement.ReplicaRole = metadata.ReplicaRole(role)
		placement.ReplicaState = metadata.ReplicaState(state)
		placement.IsPrimary = isPrimary
		placement.StoredSize = storedSize
		placement.ChecksumState = checksumState
		if lastSyncAt.Valid {
			t := lastSyncAt.Time
			placement.LastSyncAt = &t
		}
		if err := unmarshalJSON(chunkIDsBytes, &placement.ChunkIDs); err != nil {
			return err
		}
		if file.NodePlacements == nil {
			file.NodePlacements = make(metadata.NodePlacements)
		}
		file.NodePlacements[metadata.NodeID(nodeID)] = placement
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("postgres repository: iterate file placements: %w", err)
	}
	return nil
}

func marshalJSON[T any](value T, zero T) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("postgres repository: marshal json: %w", err)
	}
	if string(data) == "null" {
		data, err = json.Marshal(zero)
		if err != nil {
			return nil, fmt.Errorf("postgres repository: marshal zero json: %w", err)
		}
	}
	return data, nil
}

func unmarshalJSON[T any](data []byte, dst *T) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("postgres repository: unmarshal json: %w", err)
	}
	return nil
}

func containsFileStatus(values []metadata.FileStatus, target metadata.FileStatus) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
