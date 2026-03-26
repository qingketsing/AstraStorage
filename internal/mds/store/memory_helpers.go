package store

import (
	"slices"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
)

// cloneState 深拷贝整个仓储状态，是内存事务语义成立的基础。
func cloneState(state memoryState) memoryState {
	cloned := memoryState{
		inodes:         make(map[metadata.InodeID]*metadata.InodeMetadata, len(state.inodes)),
		files:          make(map[metadata.FileID]*metadata.FileMetadata, len(state.files)),
		chunks:         make(map[metadata.ChunkID]*metadata.ChunkMetadata, len(state.chunks)),
		uploadSessions: make(map[metadata.UploadSessionID]*metadata.UploadSession, len(state.uploadSessions)),
		nodes:          make(map[metadata.NodeID]*metadata.NodeInfo, len(state.nodes)),
		replicaPlans:   make(map[string]*metadata.ReplicaPlan, len(state.replicaPlans)),
	}
	for id, inode := range state.inodes {
		cloned.inodes[id] = cloneInode(inode)
	}
	for id, file := range state.files {
		cloned.files[id] = cloneFile(file)
	}
	for id, chunk := range state.chunks {
		copyChunk := cloneChunk(*chunk)
		cloned.chunks[id] = &copyChunk
	}
	for id, session := range state.uploadSessions {
		cloned.uploadSessions[id] = cloneUploadSession(session)
	}
	for id, node := range state.nodes {
		copyNode := cloneNodeInfo(*node)
		cloned.nodes[id] = &copyNode
	}
	for id, plan := range state.replicaPlans {
		cloned.replicaPlans[id] = cloneReplicaPlan(plan)
	}
	return cloned
}

// findInode 支持通过 ID、父节点、路径、名称和类型组合检索 inode。
// 为避免歧义，调用方应尽量使用足够明确的 selector。
func findInode(state memoryState, selector InodeSelector) (*metadata.InodeMetadata, bool) {
	if selector.ID != "" {
		inode, ok := state.inodes[selector.ID]
		return inode, ok
	}
	for _, inode := range state.inodes {
		if selector.ParentID != "" && inode.ParentID != selector.ParentID {
			continue
		}
		if selector.Path != "" && inode.Path != selector.Path {
			continue
		}
		if selector.Name != "" && inode.Name != selector.Name {
			continue
		}
		if selector.Type != nil && inode.Type != *selector.Type {
			continue
		}
		return inode, true
	}
	return nil, false
}

// findFile 支持通过 ID、inode、路径、名称等字段检索文件记录。
func findFile(state memoryState, selector FileSelector) (*metadata.FileMetadata, bool) {
	if selector.ID != "" {
		file, ok := state.files[selector.ID]
		return file, ok
	}
	for _, file := range state.files {
		if selector.InodeID != "" && file.InodeID != selector.InodeID {
			continue
		}
		if selector.ParentInodeID != "" && file.ParentInodeID != selector.ParentInodeID {
			continue
		}
		if selector.Namespace != "" && file.Namespace != selector.Namespace {
			continue
		}
		if selector.Path != "" && file.Path != selector.Path {
			continue
		}
		if selector.Name != "" && file.Name != selector.Name {
			continue
		}
		if selector.Version != nil && file.Version != *selector.Version {
			continue
		}
		return file, true
	}
	return nil, false
}

// findChunk 支持通过 chunk ID 或 fileID + index 组合检索 chunk。
func findChunk(state memoryState, selector ChunkSelector) (*metadata.ChunkMetadata, bool) {
	if selector.ID != "" {
		chunk, ok := state.chunks[selector.ID]
		return chunk, ok
	}
	for _, chunk := range state.chunks {
		if selector.FileID != "" && chunk.FileID != selector.FileID {
			continue
		}
		if selector.Index != nil && chunk.Index != *selector.Index {
			continue
		}
		return chunk, true
	}
	return nil, false
}

func nameTaken(state *memoryState, parentID metadata.InodeID, name string, excludeID metadata.InodeID) bool {
	for _, inode := range state.inodes {
		if inode.ParentID == parentID && inode.Name == name && inode.ID != excludeID && inode.Status != metadata.InodeStatusDeleted {
			return true
		}
	}
	return false
}

// isDescendant 用于判断 candidateID 是否位于 ancestorID 的子树中。
// 这个判断主要服务于“目录不能移动到自己的后代下面”这条约束。
func isDescendant(state memoryState, candidateID, ancestorID metadata.InodeID) bool {
	current, ok := state.inodes[candidateID]
	for ok {
		if current.ParentID == ancestorID {
			return true
		}
		current, ok = state.inodes[current.ParentID]
	}
	return false
}

// fileHasNode 判断某个节点是否出现在文件级放置结果中。
func fileHasNode(file *metadata.FileMetadata, nodeID metadata.NodeID) bool {
	if file.PrimaryNodeID == nodeID {
		return true
	}
	if slices.Contains(file.SecondaryNodeIDs, nodeID) {
		return true
	}
	_, ok := file.NodePlacements[nodeID]
	return ok
}

// hasLabels 用于判断节点 labels 是否包含给定筛选条件。
func hasLabels(actual, expected map[string]string) bool {
	for key, value := range expected {
		if actual[key] != value {
			return false
		}
	}
	return true
}

// joinPath 把父路径和名称规范化拼接成绝对路径。
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

// 下面这些 clone 辅助函数统一负责深拷贝 map、slice、time 指针等引用型字段，
// 目的是避免外部代码通过返回值直接修改仓储内部状态。
func cloneInode(inode *metadata.InodeMetadata) *metadata.InodeMetadata {
	if inode == nil {
		return nil
	}
	copyInode := *inode
	if inode.AccessedAt != nil {
		t := *inode.AccessedAt
		copyInode.AccessedAt = &t
	}
	return &copyInode
}

func cloneFile(file *metadata.FileMetadata) *metadata.FileMetadata {
	if file == nil {
		return nil
	}
	copyFile := *file
	copyFile.SecondaryNodeIDs = append([]metadata.NodeID(nil), file.SecondaryNodeIDs...)
	copyFile.Checksum = cloneChecksum(file.Checksum)
	copyFile.ReplicaPolicy = file.ReplicaPolicy
	copyFile.UserMetadata = cloneStringMap(file.UserMetadata)
	copyFile.Tags = cloneStringMap(file.Tags)
	copyFile.NodePlacements = cloneNodePlacements(file.NodePlacements)
	if file.CompletedAt != nil {
		t := *file.CompletedAt
		copyFile.CompletedAt = &t
	}
	return &copyFile
}

func cloneUploadSession(session *metadata.UploadSession) *metadata.UploadSession {
	if session == nil {
		return nil
	}
	copySession := *session
	if session.ExpectedChecksum != nil {
		checksum := cloneChecksum(*session.ExpectedChecksum)
		copySession.ExpectedChecksum = &checksum
	}
	if session.VerifiedChecksum != nil {
		checksum := cloneChecksum(*session.VerifiedChecksum)
		copySession.VerifiedChecksum = &checksum
	}
	copySession.ClientMetadata = cloneStringMap(session.ClientMetadata)
	copySession.TransportAttributes = cloneStringMap(session.TransportAttributes)
	if session.ExpiresAt != nil {
		t := *session.ExpiresAt
		copySession.ExpiresAt = &t
	}
	if session.CompletedAt != nil {
		t := *session.CompletedAt
		copySession.CompletedAt = &t
	}
	if session.Retry.LastFailureAt != nil {
		t := *session.Retry.LastFailureAt
		copySession.Retry.LastFailureAt = &t
	}
	if session.Retry.NextRetryAt != nil {
		t := *session.Retry.NextRetryAt
		copySession.Retry.NextRetryAt = &t
	}
	return &copySession
}

func cloneChunk(chunk metadata.ChunkMetadata) metadata.ChunkMetadata {
	chunk.Checksum = cloneChecksum(chunk.Checksum)
	chunk.ReplicaPolicy = chunk.ReplicaPolicy
	chunk.Replicas = cloneReplicaSet(chunk.Replicas)
	if chunk.VerifiedAt != nil {
		t := *chunk.VerifiedAt
		chunk.VerifiedAt = &t
	}
	return chunk
}

func cloneNodeInfo(node metadata.NodeInfo) metadata.NodeInfo {
	node.Labels = cloneStringMap(node.Labels)
	if node.LastSeenAt != nil {
		t := *node.LastSeenAt
		node.LastSeenAt = &t
	}
	return node
}

func cloneReplicaPlan(plan *metadata.ReplicaPlan) *metadata.ReplicaPlan {
	if plan == nil {
		return nil
	}
	copyPlan := *plan
	if plan.NextRetryAt != nil {
		t := *plan.NextRetryAt
		copyPlan.NextRetryAt = &t
	}
	if plan.CompletedAt != nil {
		t := *plan.CompletedAt
		copyPlan.CompletedAt = &t
	}
	return &copyPlan
}

func cloneChecksum(checksum metadata.Checksum) metadata.Checksum {
	if checksum.VerifiedAt != nil {
		t := *checksum.VerifiedAt
		checksum.VerifiedAt = &t
	}
	return checksum
}

func cloneReplicaSet(set metadata.ReplicaSet) metadata.ReplicaSet {
	if set == nil {
		return nil
	}
	cloned := make(metadata.ReplicaSet, len(set))
	for nodeID, replica := range set {
		cloned[nodeID] = cloneReplica(replica)
	}
	return cloned
}

func cloneReplica(replica metadata.ReplicaMetadata) metadata.ReplicaMetadata {
	replica.Checksum = cloneChecksum(replica.Checksum)
	if replica.VerifiedAt != nil {
		t := *replica.VerifiedAt
		replica.VerifiedAt = &t
	}
	return replica
}

func cloneNodePlacements(placements metadata.NodePlacements) metadata.NodePlacements {
	if placements == nil {
		return nil
	}
	cloned := make(metadata.NodePlacements, len(placements))
	for nodeID, placement := range placements {
		cloned[nodeID] = cloneNodePlacement(placement)
	}
	return cloned
}

func cloneNodePlacement(placement metadata.NodePlacement) metadata.NodePlacement {
	placement.Node = cloneNodeInfo(placement.Node)
	placement.ChunkIDs = append([]metadata.ChunkID(nil), placement.ChunkIDs...)
	if placement.LastSyncAt != nil {
		t := *placement.LastSyncAt
		placement.LastSyncAt = &t
	}
	return placement
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func applyListWindow[T any](items []T, limit, offset int) []T {
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return []T{}
	}
	items = items[offset:]
	if limit <= 0 || limit >= len(items) {
		return items
	}
	return items[:limit]
}
