package store

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"AstraStorage/internal/mds/metadata"
)

func (r *memoryRepository) CreateInode(_ context.Context, inode *metadata.InodeMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return createInode(&r.state, inode)
}

func (tx *memoryTx) CreateInode(_ context.Context, inode *metadata.InodeMetadata) error {
	return createInode(&tx.state, inode)
}

// createInode 实现目录树节点创建的核心约束。
// 这里重点保证：
// - 根目录只能创建一次
// - 非根节点必须挂在已存在的目录节点下
// - 同一个父目录下名称不能重复
// - 目录节点不应携带 FileID
func createInode(state *memoryState, inode *metadata.InodeMetadata) error {
	if inode == nil {
		return fmt.Errorf("%w: inode is nil", ErrInvalidArgument)
	}
	if inode.ID == "" {
		return fmt.Errorf("%w: inode id is required", ErrInvalidArgument)
	}
	if inode.Name == "" && inode.ID != metadata.InodeID(metadata.RootInodeID) {
		return fmt.Errorf("%w: inode name is required", ErrInvalidArgument)
	}
	if inode.Type != metadata.InodeTypeDirectory && inode.Type != metadata.InodeTypeFile {
		return fmt.Errorf("%w: inode type is required", ErrInvalidArgument)
	}
	if _, exists := state.inodes[inode.ID]; exists {
		return fmt.Errorf("%w: inode id %q", ErrAlreadyExists, inode.ID)
	}
	if inode.ID == metadata.InodeID(metadata.RootInodeID) {
		if inode.ParentID != "" {
			return fmt.Errorf("%w: root inode cannot have a parent", ErrInvalidArgument)
		}
		for _, existing := range state.inodes {
			if existing.ID == metadata.InodeID(metadata.RootInodeID) {
				return fmt.Errorf("%w: root inode already exists", ErrAlreadyExists)
			}
		}
	} else {
		parent, ok := state.inodes[inode.ParentID]
		if !ok {
			return fmt.Errorf("%w: parent inode %q", ErrNotFound, inode.ParentID)
		}
		if parent.Type != metadata.InodeTypeDirectory {
			return fmt.Errorf("%w: parent inode %q is not a directory", ErrInvalidArgument, inode.ParentID)
		}
	}
	if nameTaken(state, inode.ParentID, inode.Name, inode.ID) {
		return fmt.Errorf("%w: duplicate name %q under parent %q", ErrAlreadyExists, inode.Name, inode.ParentID)
	}
	if inode.Type == metadata.InodeTypeDirectory {
		inode.FileID = ""
	}
	copyInode := cloneInode(inode)
	state.inodes[copyInode.ID] = copyInode
	return nil
}

func (r *memoryRepository) GetInode(_ context.Context, selector InodeSelector) (*metadata.InodeMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return getInode(r.state, selector)
}

func (tx *memoryTx) GetInode(_ context.Context, selector InodeSelector) (*metadata.InodeMetadata, error) {
	return getInode(tx.state, selector)
}

// getInode 返回深拷贝后的结果，避免外部直接修改仓储内部状态。
func getInode(state memoryState, selector InodeSelector) (*metadata.InodeMetadata, error) {
	inode, ok := findInode(state, selector)
	if !ok {
		return nil, fmt.Errorf("%w: inode", ErrNotFound)
	}
	return cloneInode(inode), nil
}

func (r *memoryRepository) ListChildren(_ context.Context, parentID metadata.InodeID, opts ListOptions) ([]metadata.DirectoryEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listChildren(r.state, parentID, opts)
}

func (tx *memoryTx) ListChildren(_ context.Context, parentID metadata.InodeID, opts ListOptions) ([]metadata.DirectoryEntry, error) {
	return listChildren(tx.state, parentID, opts)
}

// listChildren 返回目录项视图，而不是完整 inode 记录。
// deleted 状态节点会从正常列目录结果中被过滤掉。
func listChildren(state memoryState, parentID metadata.InodeID, opts ListOptions) ([]metadata.DirectoryEntry, error) {
	if parentID == "" {
		return nil, fmt.Errorf("%w: parent id is required", ErrInvalidArgument)
	}
	if _, ok := state.inodes[parentID]; !ok {
		return nil, fmt.Errorf("%w: parent inode %q", ErrNotFound, parentID)
	}

	entries := make([]metadata.DirectoryEntry, 0)
	for _, inode := range state.inodes {
		if inode.ParentID != parentID || inode.Status == metadata.InodeStatusDeleted {
			continue
		}
		entries = append(entries, metadata.DirectoryEntry{
			ParentID:  inode.ParentID,
			ChildID:   inode.ID,
			Name:      inode.Name,
			Type:      inode.Type,
			CreatedAt: inode.CreatedAt,
			UpdatedAt: inode.UpdatedAt,
		})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return applyListWindow(entries, opts.Limit, opts.Offset), nil
}

func (r *memoryRepository) UpdateInode(_ context.Context, patch InodePatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateInode(&r.state, patch)
}

func (tx *memoryTx) UpdateInode(_ context.Context, patch InodePatch) error {
	return updateInode(&tx.state, patch)
}

// updateInode 执行 inode 的局部更新。
// 它只负责字段层面的存在性和重名校验，不在这里协调更高层跨模块一致性。
func updateInode(state *memoryState, patch InodePatch) error {
	inode, ok := findInode(*state, patch.Selector)
	if !ok {
		return fmt.Errorf("%w: inode", ErrNotFound)
	}

	if patch.Name != nil && *patch.Name != inode.Name && nameTaken(state, inode.ParentID, *patch.Name, inode.ID) {
		return fmt.Errorf("%w: duplicate name %q under parent %q", ErrAlreadyExists, *patch.Name, inode.ParentID)
	}
	if patch.Path != nil {
		inode.Path = *patch.Path
	}
	if patch.Name != nil {
		inode.Name = *patch.Name
	}
	if patch.ParentID != nil {
		inode.ParentID = *patch.ParentID
	}
	if patch.Status != nil {
		inode.Status = *patch.Status
	}
	if patch.Size != nil {
		inode.Size = *patch.Size
	}
	if patch.Permissions != nil {
		inode.Permissions = *patch.Permissions
	}
	if patch.Owner != nil {
		inode.Owner = *patch.Owner
	}
	if patch.Group != nil {
		inode.Group = *patch.Group
	}
	if patch.LinkCount != nil {
		inode.LinkCount = *patch.LinkCount
	}
	if patch.Generation != nil {
		inode.Generation = *patch.Generation
	}
	if patch.AccessedAt != nil {
		t := *patch.AccessedAt
		inode.AccessedAt = &t
	}
	inode.UpdatedAt = patch.UpdatedAt
	return nil
}

func (r *memoryRepository) MoveInode(_ context.Context, op MoveInodeOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return moveInode(&r.state, op)
}

func (tx *memoryTx) MoveInode(_ context.Context, op MoveInodeOperation) error {
	return moveInode(&tx.state, op)
}

// moveInode 处理迁移到新父目录的场景，必要时可以同时改名。
// 这里只更新目标节点自身的 ParentID、Name 和 Path；
// 如果移动的是目录，子树 Path 仍需由 UpdateSubtreePaths 批量更新。
func moveInode(state *memoryState, op MoveInodeOperation) error {
	inode, ok := findInode(*state, op.Selector)
	if !ok {
		return fmt.Errorf("%w: inode", ErrNotFound)
	}
	if inode.Status == metadata.InodeStatusDeleting || inode.Status == metadata.InodeStatusDeleted {
		return fmt.Errorf("%w: inode %q cannot be moved", ErrConflict, inode.ID)
	}
	if op.ExpectedType != nil && inode.Type != *op.ExpectedType {
		return fmt.Errorf("%w: inode type mismatch", ErrConflict)
	}
	parent, ok := state.inodes[op.TargetParentID]
	if !ok {
		return fmt.Errorf("%w: parent inode %q", ErrNotFound, op.TargetParentID)
	}
	if parent.Type != metadata.InodeTypeDirectory {
		return fmt.Errorf("%w: target parent must be a directory", ErrInvalidArgument)
	}
	if inode.ID == op.TargetParentID {
		return fmt.Errorf("%w: inode cannot be its own parent", ErrInvalidArgument)
	}
	if inode.Type == metadata.InodeTypeDirectory && isDescendant(*state, op.TargetParentID, inode.ID) {
		return fmt.Errorf("%w: cannot move directory into its descendant", ErrInvalidArgument)
	}
	newName := op.NewName
	if newName == "" {
		newName = inode.Name
	}
	if nameTaken(state, op.TargetParentID, newName, inode.ID) {
		return fmt.Errorf("%w: duplicate name %q under parent %q", ErrAlreadyExists, newName, op.TargetParentID)
	}

	inode.ParentID = op.TargetParentID
	inode.Name = newName
	inode.Path = joinPath(op.TargetParentPath, newName)
	inode.UpdatedAt = op.UpdatedAt
	return nil
}

func (r *memoryRepository) RenameInode(_ context.Context, op RenameInodeOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return renameInode(&r.state, op)
}

func (tx *memoryTx) RenameInode(_ context.Context, op RenameInodeOperation) error {
	return renameInode(&tx.state, op)
}

// renameInode 只处理同一父目录下的重命名。
// 目录 rename 后，调用方应继续调用 UpdateSubtreePaths 维护子树路径缓存。
func renameInode(state *memoryState, op RenameInodeOperation) error {
	inode, ok := findInode(*state, op.Selector)
	if !ok {
		return fmt.Errorf("%w: inode", ErrNotFound)
	}
	if inode.Status == metadata.InodeStatusDeleting || inode.Status == metadata.InodeStatusDeleted {
		return fmt.Errorf("%w: inode %q cannot be renamed", ErrConflict, inode.ID)
	}
	if op.ExpectedType != nil && inode.Type != *op.ExpectedType {
		return fmt.Errorf("%w: inode type mismatch", ErrConflict)
	}
	if op.NewName == "" {
		return fmt.Errorf("%w: new name is required", ErrInvalidArgument)
	}
	if nameTaken(state, inode.ParentID, op.NewName, inode.ID) {
		return fmt.Errorf("%w: duplicate name %q under parent %q", ErrAlreadyExists, op.NewName, inode.ParentID)
	}
	inode.Name = op.NewName
	inode.Path = replaceBaseName(inode.Path, op.NewName)
	inode.UpdatedAt = op.UpdatedAt
	return nil
}

func (r *memoryRepository) DeleteInode(_ context.Context, selector InodeSelector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return deleteInode(&r.state, selector)
}

func (tx *memoryTx) DeleteInode(_ context.Context, selector InodeSelector) error {
	return deleteInode(&tx.state, selector)
}

// deleteInode 当前只允许删除空节点。
// 如果将来要支持递归删除，应该由显式事务流程统一驱动。
func deleteInode(state *memoryState, selector InodeSelector) error {
	inode, ok := findInode(*state, selector)
	if !ok {
		return fmt.Errorf("%w: inode", ErrNotFound)
	}
	for _, child := range state.inodes {
		if child.ParentID == inode.ID && child.Status != metadata.InodeStatusDeleted {
			return fmt.Errorf("%w: inode %q has children", ErrConflict, inode.ID)
		}
	}
	delete(state.inodes, inode.ID)
	return nil
}

func (r *memoryRepository) UpdateSubtreePaths(_ context.Context, op UpdateSubtreePathsOperation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateSubtreePaths(&r.state, op)
}

func (tx *memoryTx) UpdateSubtreePaths(_ context.Context, op UpdateSubtreePathsOperation) error {
	return updateSubtreePaths(&tx.state, op)
}

// updateSubtreePaths 用于目录 rename 或 move 后批量更新子树路径缓存。
// 目录树的真实结构仍然由 ID + ParentID + Name 决定，Path 只是查询优化字段。
func updateSubtreePaths(state *memoryState, op UpdateSubtreePathsOperation) error {
	if op.RootID == "" {
		return fmt.Errorf("%w: root id is required", ErrInvalidArgument)
	}
	root, ok := state.inodes[op.RootID]
	if !ok {
		return fmt.Errorf("%w: root inode %q", ErrNotFound, op.RootID)
	}
	for _, inode := range state.inodes {
		if inode.ID == root.ID {
			continue
		}
		if !strings.HasPrefix(inode.Path, op.OldPrefix) {
			continue
		}
		if !isDescendant(*state, inode.ID, root.ID) {
			continue
		}
		inode.Path = op.NewPrefix + strings.TrimPrefix(inode.Path, op.OldPrefix)
		inode.UpdatedAt = op.UpdatedAt
	}
	return nil
}
