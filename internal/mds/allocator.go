package mds

import (
	"sort"
	"strings"

	"AstraStorage/internal/mds/metadata"
)

type NodeSelectionInput struct {
	Candidates []metadata.NodeInfo
	Excluded   map[metadata.NodeID]struct{}
	Count      int
}

type PlacementRequest struct {
	Candidates    []metadata.NodeInfo
	Excluded      map[metadata.NodeID]struct{}
	RequiredBytes int64
	Count         int
}

func SelectCapacityAwareNodes(input NodeSelectionInput) []metadata.NodeInfo {
	return SelectPlacementTargets(PlacementRequest{
		Candidates: input.Candidates,
		Excluded:   input.Excluded,
		Count:      input.Count,
	})
}

func SelectPlacementTargets(req PlacementRequest) []metadata.NodeInfo {
	if req.Count <= 0 || len(req.Candidates) == 0 {
		return nil
	}

	selected := make([]metadata.NodeInfo, 0, len(req.Candidates))
	for _, node := range req.Candidates {
		if !node.Healthy {
			continue
		}
		if strings.TrimSpace(node.Address) == "" {
			continue
		}
		if _, excluded := req.Excluded[node.ID]; excluded {
			continue
		}
		if node.Capacity < 0 || node.Used < 0 {
			continue
		}
		if node.Used > node.Capacity {
			continue
		}
		if availableCapacity(node) <= 0 {
			continue
		}
		if availableCapacity(node) < req.RequiredBytes {
			continue
		}
		selected = append(selected, node)
	}

	sort.Slice(selected, func(i, j int) bool {
		leftAvailable := availableCapacity(selected[i])
		rightAvailable := availableCapacity(selected[j])
		if leftAvailable != rightAvailable {
			return leftAvailable > rightAvailable
		}
		return selected[i].ID < selected[j].ID
	})

	if req.Count < len(selected) {
		selected = selected[:req.Count]
	}
	return selected
}

func RequiredPlacementBytes(chunk metadata.ChunkMetadata) int64 {
	var maxReadyStoredSize int64
	for _, replica := range chunk.Replicas {
		if replica.State != metadata.ReplicaStateReady {
			continue
		}
		if replica.StoredSize > maxReadyStoredSize {
			maxReadyStoredSize = replica.StoredSize
		}
	}
	if maxReadyStoredSize > 0 {
		return maxReadyStoredSize
	}
	if chunk.Size > 0 {
		return chunk.Size
	}
	return 0
}

func CountEffectiveReadyReplicas(chunk metadata.ChunkMetadata, nodeIndex map[metadata.NodeID]metadata.NodeInfo) int {
	count := 0
	for nodeID, replica := range chunk.Replicas {
		if replica.State != metadata.ReplicaStateReady {
			continue
		}
		node, ok := nodeIndex[nodeID]
		if !ok || !node.Healthy {
			continue
		}
		count++
	}
	return count
}

func BuildReplicaExclusionSet(chunk metadata.ChunkMetadata) map[metadata.NodeID]struct{} {
	if len(chunk.Replicas) == 0 {
		return nil
	}
	excluded := make(map[metadata.NodeID]struct{}, len(chunk.Replicas))
	for nodeID := range chunk.Replicas {
		excluded[nodeID] = struct{}{}
	}
	return excluded
}

func availableCapacity(node metadata.NodeInfo) int64 {
	return node.Capacity - node.Used
}
