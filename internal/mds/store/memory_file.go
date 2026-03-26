package store

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"AstraStorage/internal/mds/metadata"
)

func (r *memoryRepository) CreateFile(_ context.Context, file *metadata.FileMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return createFile(&r.state, file)
}

func (tx *memoryTx) CreateFile(_ context.Context, file *metadata.FileMetadata) error {
	return createFile(&tx.state, file)
}

// createFile 把文件元数据挂到一个文件型 inode 上。
// 当前固定使用 4 MiB chunk size，这和 architecture 文档中的约束保持一致。
func createFile(state *memoryState, file *metadata.FileMetadata) error {
	if file == nil {
		return fmt.Errorf("%w: file is nil", ErrInvalidArgument)
	}
	if file.ID == "" || file.InodeID == "" {
		return fmt.Errorf("%w: file id and inode id are required", ErrInvalidArgument)
	}
	if _, exists := state.files[file.ID]; exists {
		return fmt.Errorf("%w: file id %q", ErrAlreadyExists, file.ID)
	}
	for _, existing := range state.files {
		if existing.InodeID == file.InodeID {
			return fmt.Errorf("%w: inode %q already has file %q", ErrAlreadyExists, file.InodeID, existing.ID)
		}
	}
	inode, ok := state.inodes[file.InodeID]
	if !ok {
		return fmt.Errorf("%w: inode %q", ErrNotFound, file.InodeID)
	}
	if inode.Type != metadata.InodeTypeFile {
		return fmt.Errorf("%w: inode %q is not a file", ErrInvalidArgument, inode.ID)
	}
	if file.ChunkSize == 0 {
		file.ChunkSize = metadata.FixedChunkSizeBytes
	}
	if file.ChunkSize != metadata.FixedChunkSizeBytes {
		return fmt.Errorf("%w: chunk size must be %d", ErrInvalidArgument, metadata.FixedChunkSizeBytes)
	}
	copyFile := cloneFile(file)
	state.files[copyFile.ID] = copyFile
	return nil
}

func (r *memoryRepository) GetFile(_ context.Context, selector FileSelector) (*metadata.FileMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return getFile(r.state, selector)
}

func (tx *memoryTx) GetFile(_ context.Context, selector FileSelector) (*metadata.FileMetadata, error) {
	return getFile(tx.state, selector)
}

// getFile 返回的是文件记录副本，避免调用方直接持有内部 map、slice 和指针字段。
func getFile(state memoryState, selector FileSelector) (*metadata.FileMetadata, error) {
	file, ok := findFile(state, selector)
	if !ok {
		return nil, fmt.Errorf("%w: file", ErrNotFound)
	}
	return cloneFile(file), nil
}

func (r *memoryRepository) ListFiles(_ context.Context, filter FileFilter) ([]*metadata.FileMetadata, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listFiles(r.state, filter)
}

func (tx *memoryTx) ListFiles(_ context.Context, filter FileFilter) ([]*metadata.FileMetadata, error) {
	return listFiles(tx.state, filter)
}

// listFiles 提供按命名空间、父 inode、路径前缀、状态和节点的基础筛选能力。
func listFiles(state memoryState, filter FileFilter) ([]*metadata.FileMetadata, error) {
	files := make([]*metadata.FileMetadata, 0)
	for _, file := range state.files {
		if filter.Namespace != "" && file.Namespace != filter.Namespace {
			continue
		}
		if filter.ParentInodeID != "" && file.ParentInodeID != filter.ParentInodeID {
			continue
		}
		if filter.PathPrefix != "" && !strings.HasPrefix(file.Path, filter.PathPrefix) {
			continue
		}
		if len(filter.Status) > 0 && !slices.Contains(filter.Status, file.Status) {
			continue
		}
		if filter.NodeID != "" && !fileHasNode(file, filter.NodeID) {
			continue
		}
		files = append(files, cloneFile(file))
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return applyListWindow(files, filter.Limit, filter.Offset), nil
}

func (r *memoryRepository) UpdateFile(_ context.Context, patch FilePatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateFile(&r.state, patch)
}

func (tx *memoryTx) UpdateFile(_ context.Context, patch FilePatch) error {
	return updateFile(&tx.state, patch)
}

// updateFile 负责文件记录的局部字段更新。
// 这里仍然坚持 chunk size 必须是固定值，不允许被改成其他大小。
func updateFile(state *memoryState, patch FilePatch) error {
	file, ok := findFile(*state, patch.Selector)
	if !ok {
		return fmt.Errorf("%w: file", ErrNotFound)
	}
	if patch.Size != nil {
		file.Size = *patch.Size
	}
	if patch.ParentInodeID != nil {
		file.ParentInodeID = *patch.ParentInodeID
	}
	if patch.Path != nil {
		file.Path = *patch.Path
	}
	if patch.Name != nil {
		file.Name = *patch.Name
	}
	if patch.StoredSize != nil {
		file.StoredSize = *patch.StoredSize
	}
	if patch.ChunkSize != nil {
		if *patch.ChunkSize != metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: chunk size must be %d", ErrInvalidArgument, metadata.FixedChunkSizeBytes)
		}
		file.ChunkSize = *patch.ChunkSize
	}
	if patch.Version != nil {
		file.Version = *patch.Version
	}
	if patch.Status != nil {
		file.Status = *patch.Status
	}
	if patch.PrimaryNodeID != nil {
		file.PrimaryNodeID = *patch.PrimaryNodeID
	}
	if patch.SecondaryNodeIDs != nil {
		file.SecondaryNodeIDs = append([]metadata.NodeID(nil), patch.SecondaryNodeIDs...)
	}
	if patch.LatestUploadSessionID != nil {
		file.LatestUploadSessionID = *patch.LatestUploadSessionID
	}
	if patch.Checksum != nil {
		file.Checksum = cloneChecksum(*patch.Checksum)
	}
	if patch.ReplicaPolicy != nil {
		file.ReplicaPolicy = *patch.ReplicaPolicy
	}
	if patch.UserMetadata != nil {
		file.UserMetadata = cloneStringMap(patch.UserMetadata)
	}
	if patch.Tags != nil {
		file.Tags = cloneStringMap(patch.Tags)
	}
	if patch.CompletedAt != nil {
		t := *patch.CompletedAt
		file.CompletedAt = &t
	}
	file.UpdatedAt = patch.UpdatedAt
	return nil
}

func (r *memoryRepository) UpdateFilePlacements(_ context.Context, patch FilePlacementPatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateFilePlacements(&r.state, patch)
}

func (tx *memoryTx) UpdateFilePlacements(_ context.Context, patch FilePlacementPatch) error {
	return updateFilePlacements(&tx.state, patch)
}

// updateFilePlacements 维护文件级节点放置信息。
// 当前只做集合更新和状态门禁，更复杂的副本一致性校验由后续实现补充。
func updateFilePlacements(state *memoryState, patch FilePlacementPatch) error {
	file, ok := findFile(*state, patch.Selector)
	if !ok {
		return fmt.Errorf("%w: file", ErrNotFound)
	}
	if len(patch.ExpectedStatus) > 0 && !slices.Contains(patch.ExpectedStatus, file.Status) {
		return fmt.Errorf("%w: file status mismatch", ErrConflict)
	}
	if file.NodePlacements == nil {
		file.NodePlacements = make(metadata.NodePlacements)
	}
	for nodeID, placement := range patch.Upserts {
		file.NodePlacements[nodeID] = cloneNodePlacement(placement)
	}
	for _, nodeID := range patch.RemoveNodeIDs {
		delete(file.NodePlacements, nodeID)
	}
	file.UpdatedAt = patch.UpdatedAt
	return nil
}

func (r *memoryRepository) DeleteFile(_ context.Context, selector FileSelector) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return deleteFile(&r.state, selector)
}

func (tx *memoryTx) DeleteFile(_ context.Context, selector FileSelector) error {
	return deleteFile(&tx.state, selector)
}

// deleteFile 目前只删除 file 记录本身。
// 实际删除流程通常还需要和 inode、chunk、session 清理放在同一个事务里。
func deleteFile(state *memoryState, selector FileSelector) error {
	file, ok := findFile(*state, selector)
	if !ok {
		return fmt.Errorf("%w: file", ErrNotFound)
	}
	delete(state.files, file.ID)
	return nil
}
