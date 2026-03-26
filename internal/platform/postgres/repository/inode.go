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
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

const inodeColumns = `
id,
parent_id,
file_id,
path,
name,
type,
status,
size,
permissions,
owner_name,
group_name,
link_count,
generation,
created_at,
updated_at,
accessed_at
`

func (r *Repository) CreateInode(ctx context.Context, inode *metadata.InodeMetadata) error {
	return createInode(ctx, r.pool, inode)
}

func (tx *Tx) CreateInode(ctx context.Context, inode *metadata.InodeMetadata) error {
	return createInode(ctx, tx.tx, inode)
}

func (r *Repository) GetInode(ctx context.Context, selector store.InodeSelector) (*metadata.InodeMetadata, error) {
	return getInode(ctx, r.pool, selector)
}

func (tx *Tx) GetInode(ctx context.Context, selector store.InodeSelector) (*metadata.InodeMetadata, error) {
	return getInode(ctx, tx.tx, selector)
}

func (r *Repository) ListChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions) ([]metadata.DirectoryEntry, error) {
	return listChildren(ctx, r.pool, parentID, opts)
}

func (tx *Tx) ListChildren(ctx context.Context, parentID metadata.InodeID, opts store.ListOptions) ([]metadata.DirectoryEntry, error) {
	return listChildren(ctx, tx.tx, parentID, opts)
}

func (r *Repository) UpdateInode(ctx context.Context, patch store.InodePatch) error {
	return updateInode(ctx, r.pool, patch)
}

func (tx *Tx) UpdateInode(ctx context.Context, patch store.InodePatch) error {
	return updateInode(ctx, tx.tx, patch)
}

func (r *Repository) MoveInode(ctx context.Context, op store.MoveInodeOperation) error {
	return moveInode(ctx, r.pool, op)
}

func (tx *Tx) MoveInode(ctx context.Context, op store.MoveInodeOperation) error {
	return moveInode(ctx, tx.tx, op)
}

func (r *Repository) RenameInode(ctx context.Context, op store.RenameInodeOperation) error {
	return renameInode(ctx, r.pool, op)
}

func (tx *Tx) RenameInode(ctx context.Context, op store.RenameInodeOperation) error {
	return renameInode(ctx, tx.tx, op)
}

func (r *Repository) DeleteInode(ctx context.Context, selector store.InodeSelector) error {
	return deleteInode(ctx, r.pool, selector)
}

func (tx *Tx) DeleteInode(ctx context.Context, selector store.InodeSelector) error {
	return deleteInode(ctx, tx.tx, selector)
}

func (r *Repository) UpdateSubtreePaths(ctx context.Context, op store.UpdateSubtreePathsOperation) error {
	return updateSubtreePaths(ctx, r.pool, op)
}

func (tx *Tx) UpdateSubtreePaths(ctx context.Context, op store.UpdateSubtreePathsOperation) error {
	return updateSubtreePaths(ctx, tx.tx, op)
}

func createInode(ctx context.Context, db queryDB, inode *metadata.InodeMetadata) error {
	if inode == nil {
		return fmt.Errorf("%w: inode is nil", store.ErrInvalidArgument)
	}
	if inode.ID == "" {
		return fmt.Errorf("%w: inode id is required", store.ErrInvalidArgument)
	}
	if inode.Name == "" && inode.ID != metadata.InodeID(metadata.RootInodeID) {
		return fmt.Errorf("%w: inode name is required", store.ErrInvalidArgument)
	}
	if inode.Type != metadata.InodeTypeDirectory && inode.Type != metadata.InodeTypeFile {
		return fmt.Errorf("%w: inode type is required", store.ErrInvalidArgument)
	}
	if inode.ID == metadata.InodeID(metadata.RootInodeID) {
		if inode.ParentID != "" {
			return fmt.Errorf("%w: root inode cannot have a parent", store.ErrInvalidArgument)
		}
	} else {
		parent, err := getInode(ctx, db, store.InodeSelector{ID: inode.ParentID})
		if err != nil {
			return err
		}
		if parent.Type != metadata.InodeTypeDirectory {
			return fmt.Errorf("%w: parent inode %q is not a directory", store.ErrInvalidArgument, inode.ParentID)
		}
	}
	if exists, err := inodeNameTaken(ctx, db, inode.ParentID, inode.Name, inode.ID); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("%w: duplicate name %q under parent %q", store.ErrAlreadyExists, inode.Name, inode.ParentID)
	}

	fileID := inode.FileID
	if inode.Type == metadata.InodeTypeDirectory {
		fileID = ""
	}
	_, err := db.Exec(ctx, `
INSERT INTO mds_inodes (
	id, namespace, parent_id, file_id, path, name, type, status, size, permissions, owner_name, group_name,
	link_count, generation, created_at, updated_at, accessed_at
)
VALUES ($1, '', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
`,
		string(inode.ID),
		string(inode.ParentID),
		string(fileID),
		inode.Path,
		inode.Name,
		string(inode.Type),
		string(inode.Status),
		inode.Size,
		int64(inode.Permissions),
		inode.Owner,
		inode.Group,
		inode.LinkCount,
		inode.Generation,
		inode.CreatedAt,
		inode.UpdatedAt,
		inode.AccessedAt,
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func getInode(ctx context.Context, db queryDB, selector store.InodeSelector) (*metadata.InodeMetadata, error) {
	where, args, err := buildInodeSelectorWhere(selector)
	if err != nil {
		return nil, err
	}

	query := "SELECT " + inodeColumns + " FROM mds_inodes WHERE " + where + " LIMIT 1"
	inode, err := scanInode(db.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: inode", store.ErrNotFound)
		}
		return nil, err
	}
	return inode, nil
}

func listChildren(ctx context.Context, db queryDB, parentID metadata.InodeID, opts store.ListOptions) ([]metadata.DirectoryEntry, error) {
	if parentID == "" {
		return nil, fmt.Errorf("%w: parent id is required", store.ErrInvalidArgument)
	}
	if _, err := getInode(ctx, db, store.InodeSelector{ID: parentID}); err != nil {
		return nil, err
	}

	query := `
SELECT parent_id, id, name, type, created_at, updated_at
FROM mds_inodes
WHERE parent_id = $1 AND status <> $2
ORDER BY name`
	args := []any{string(parentID), string(metadata.InodeStatusDeleted)}
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", len(args)+1)
		args = append(args, opts.Limit)
	}
	if opts.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", len(args)+1)
		args = append(args, opts.Offset)
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres repository: list children query: %w", err)
	}
	defer rows.Close()

	entries := make([]metadata.DirectoryEntry, 0)
	for rows.Next() {
		var entry metadata.DirectoryEntry
		var childID string
		var parent string
		var inodeType string
		if err := rows.Scan(&parent, &childID, &entry.Name, &inodeType, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, fmt.Errorf("postgres repository: scan child entry: %w", err)
		}
		entry.ParentID = metadata.InodeID(parent)
		entry.ChildID = metadata.InodeID(childID)
		entry.Type = metadata.InodeType(inodeType)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate child entries: %w", err)
	}
	return entries, nil
}

func updateInode(ctx context.Context, db queryDB, patch store.InodePatch) error {
	inode, err := getInode(ctx, db, patch.Selector)
	if err != nil {
		return err
	}
	if patch.Name != nil && *patch.Name != inode.Name {
		exists, err := inodeNameTaken(ctx, db, inode.ParentID, *patch.Name, inode.ID)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%w: duplicate name %q under parent %q", store.ErrAlreadyExists, *patch.Name, inode.ParentID)
		}
	}

	clauses := make([]string, 0, 11)
	args := make([]any, 0, 12)
	add := func(column string, value any) {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, value)
	}

	if patch.Path != nil {
		add("path", *patch.Path)
	}
	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.ParentID != nil {
		add("parent_id", string(*patch.ParentID))
	}
	if patch.Status != nil {
		add("status", string(*patch.Status))
	}
	if patch.Size != nil {
		add("size", *patch.Size)
	}
	if patch.Permissions != nil {
		add("permissions", int64(*patch.Permissions))
	}
	if patch.Owner != nil {
		add("owner_name", *patch.Owner)
	}
	if patch.Group != nil {
		add("group_name", *patch.Group)
	}
	if patch.LinkCount != nil {
		add("link_count", *patch.LinkCount)
	}
	if patch.Generation != nil {
		add("generation", *patch.Generation)
	}
	if patch.AccessedAt != nil {
		add("accessed_at", *patch.AccessedAt)
	}
	add("updated_at", patch.UpdatedAt)
	args = append(args, string(inode.ID))

	query := "UPDATE mds_inodes SET " + strings.Join(clauses, ", ") + fmt.Sprintf(" WHERE id = $%d", len(args))
	if _, err := db.Exec(ctx, query, args...); err != nil {
		return translateExecError(err)
	}
	return nil
}

func moveInode(ctx context.Context, db queryDB, op store.MoveInodeOperation) error {
	inode, err := getInode(ctx, db, op.Selector)
	if err != nil {
		return err
	}
	if inode.Status == metadata.InodeStatusDeleting || inode.Status == metadata.InodeStatusDeleted {
		return fmt.Errorf("%w: inode %q cannot be moved", store.ErrConflict, inode.ID)
	}
	if op.ExpectedType != nil && inode.Type != *op.ExpectedType {
		return fmt.Errorf("%w: inode type mismatch", store.ErrConflict)
	}

	parent, err := getInode(ctx, db, store.InodeSelector{ID: op.TargetParentID})
	if err != nil {
		return err
	}
	if parent.Type != metadata.InodeTypeDirectory {
		return fmt.Errorf("%w: target parent must be a directory", store.ErrInvalidArgument)
	}
	if inode.ID == op.TargetParentID {
		return fmt.Errorf("%w: inode cannot be its own parent", store.ErrInvalidArgument)
	}
	if inode.Type == metadata.InodeTypeDirectory {
		descendant, err := isDescendant(ctx, db, op.TargetParentID, inode.ID)
		if err != nil {
			return err
		}
		if descendant {
			return fmt.Errorf("%w: cannot move directory into its descendant", store.ErrInvalidArgument)
		}
	}

	newName := op.NewName
	if newName == "" {
		newName = inode.Name
	}
	exists, err := inodeNameTaken(ctx, db, op.TargetParentID, newName, inode.ID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: duplicate name %q under parent %q", store.ErrAlreadyExists, newName, op.TargetParentID)
	}

	targetPath := op.TargetParentPath
	if targetPath == "" {
		targetPath = parent.Path
	}
	_, err = db.Exec(ctx, `
UPDATE mds_inodes
SET parent_id = $1, name = $2, path = $3, updated_at = $4
WHERE id = $5
`,
		string(op.TargetParentID),
		newName,
		joinPath(targetPath, newName),
		op.UpdatedAt,
		string(inode.ID),
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func renameInode(ctx context.Context, db queryDB, op store.RenameInodeOperation) error {
	inode, err := getInode(ctx, db, op.Selector)
	if err != nil {
		return err
	}
	if inode.Status == metadata.InodeStatusDeleting || inode.Status == metadata.InodeStatusDeleted {
		return fmt.Errorf("%w: inode %q cannot be renamed", store.ErrConflict, inode.ID)
	}
	if op.ExpectedType != nil && inode.Type != *op.ExpectedType {
		return fmt.Errorf("%w: inode type mismatch", store.ErrConflict)
	}
	if op.NewName == "" {
		return fmt.Errorf("%w: new name is required", store.ErrInvalidArgument)
	}

	exists, err := inodeNameTaken(ctx, db, inode.ParentID, op.NewName, inode.ID)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("%w: duplicate name %q under parent %q", store.ErrAlreadyExists, op.NewName, inode.ParentID)
	}

	_, err = db.Exec(ctx, `
UPDATE mds_inodes
SET name = $1, path = $2, updated_at = $3
WHERE id = $4
`,
		op.NewName,
		replaceBaseName(inode.Path, op.NewName),
		op.UpdatedAt,
		string(inode.ID),
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func deleteInode(ctx context.Context, db queryDB, selector store.InodeSelector) error {
	inode, err := getInode(ctx, db, selector)
	if err != nil {
		return err
	}

	var childID string
	childErr := db.QueryRow(ctx, `
SELECT id
FROM mds_inodes
WHERE parent_id = $1 AND status <> $2
LIMIT 1
`, string(inode.ID), string(metadata.InodeStatusDeleted)).Scan(&childID)
	switch {
	case childErr == nil:
		return fmt.Errorf("%w: inode %q has children", store.ErrConflict, inode.ID)
	case errors.Is(childErr, pgx.ErrNoRows):
	default:
		return fmt.Errorf("postgres repository: check inode children: %w", childErr)
	}

	if _, err := db.Exec(ctx, "DELETE FROM mds_inodes WHERE id = $1", string(inode.ID)); err != nil {
		return translateExecError(err)
	}
	return nil
}

func updateSubtreePaths(ctx context.Context, db queryDB, op store.UpdateSubtreePathsOperation) error {
	if op.RootID == "" {
		return fmt.Errorf("%w: root id is required", store.ErrInvalidArgument)
	}
	if _, err := getInode(ctx, db, store.InodeSelector{ID: op.RootID}); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("%w: root inode %q", store.ErrNotFound, op.RootID)
		}
		return err
	}

	_, err := db.Exec(ctx, `
WITH RECURSIVE subtree AS (
    SELECT id
    FROM mds_inodes
    WHERE parent_id = $1
  UNION ALL
    SELECT child.id
    FROM mds_inodes child
    INNER JOIN subtree ON child.parent_id = subtree.id
)
UPDATE mds_inodes AS inode
SET path = $2 || substring(inode.path FROM char_length($3) + 1),
    updated_at = $4
WHERE inode.id IN (SELECT id FROM subtree)
  AND inode.path LIKE $3 || '%'
`,
		string(op.RootID),
		op.NewPrefix,
		op.OldPrefix,
		op.UpdatedAt,
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func buildInodeSelectorWhere(selector store.InodeSelector) (string, []any, error) {
	clauses := make([]string, 0, 6)
	args := make([]any, 0, 6)
	add := func(column string, value any) {
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)+1))
		args = append(args, value)
	}

	if selector.ID != "" {
		add("id", string(selector.ID))
	}
	if selector.ParentID != "" {
		add("parent_id", string(selector.ParentID))
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
	if selector.Type != nil {
		add(`type`, string(*selector.Type))
	}
	if len(clauses) == 0 {
		return "", nil, fmt.Errorf("%w: inode selector is empty", store.ErrInvalidArgument)
	}
	return strings.Join(clauses, " AND "), args, nil
}

func inodeNameTaken(ctx context.Context, db queryDB, parentID metadata.InodeID, name string, excludeID metadata.InodeID) (bool, error) {
	if name == "" {
		return false, nil
	}

	query := `
SELECT id
FROM mds_inodes
WHERE parent_id = $1 AND name = $2 AND status <> $3`
	args := []any{string(parentID), name, string(metadata.InodeStatusDeleted)}
	if excludeID != "" {
		query += fmt.Sprintf(" AND id <> $%d", len(args)+1)
		args = append(args, string(excludeID))
	}
	query += " LIMIT 1"

	var id string
	err := db.QueryRow(ctx, query, args...).Scan(&id)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, pgx.ErrNoRows):
		return false, nil
	default:
		return false, fmt.Errorf("postgres repository: check inode sibling uniqueness: %w", err)
	}
}

func isDescendant(ctx context.Context, db queryDB, candidateID, ancestorID metadata.InodeID) (bool, error) {
	currentID := candidateID
	for currentID != "" {
		inode, err := getInode(ctx, db, store.InodeSelector{ID: currentID})
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if inode.ParentID == ancestorID {
			return true, nil
		}
		currentID = inode.ParentID
	}
	return false, nil
}

func scanInode(row rowScanner) (*metadata.InodeMetadata, error) {
	var inode metadata.InodeMetadata
	var id string
	var parentID string
	var fileID string
	var inodeType string
	var status string
	var permissions int64
	var accessedAt sql.NullTime

	if err := row.Scan(
		&id,
		&parentID,
		&fileID,
		&inode.Path,
		&inode.Name,
		&inodeType,
		&status,
		&inode.Size,
		&permissions,
		&inode.Owner,
		&inode.Group,
		&inode.LinkCount,
		&inode.Generation,
		&inode.CreatedAt,
		&inode.UpdatedAt,
		&accessedAt,
	); err != nil {
		return nil, err
	}

	inode.ID = metadata.InodeID(id)
	inode.ParentID = metadata.InodeID(parentID)
	inode.FileID = metadata.FileID(fileID)
	inode.Type = metadata.InodeType(inodeType)
	inode.Status = metadata.InodeStatus(status)
	inode.Permissions = uint32(permissions)
	if accessedAt.Valid {
		t := accessedAt.Time
		inode.AccessedAt = &t
	}
	return &inode, nil
}

func translateExecError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation:
			return fmt.Errorf("%w: %s", store.ErrAlreadyExists, pgErr.ConstraintName)
		case pgForeignKeyViolation:
			return fmt.Errorf("%w: %s", store.ErrConflict, pgErr.ConstraintName)
		}
	}
	return err
}

func joinPath(parentPath, name string) string {
	if parentPath == "" || parentPath == "/" {
		return "/" + name
	}
	return strings.TrimRight(parentPath, "/") + "/" + name
}

func replaceBaseName(path, name string) string {
	if path == "/" {
		return path
	}
	parent := path[:strings.LastIndex(path, "/")]
	if parent == "" {
		parent = "/"
	}
	return joinPath(parent, name)
}
