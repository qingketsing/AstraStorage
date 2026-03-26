package store

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"AstraStorage/internal/mds/metadata"
)

func (r *memoryRepository) UpsertNode(_ context.Context, node metadata.NodeInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return upsertNode(&r.state, node)
}

func (tx *memoryTx) UpsertNode(_ context.Context, node metadata.NodeInfo) error {
	return upsertNode(&tx.state, node)
}

// upsertNode 允许节点记录被重复写入，用于支持节点元信息刷新和心跳场景。
func upsertNode(state *memoryState, node metadata.NodeInfo) error {
	if node.ID == "" {
		return fmt.Errorf("%w: node id is required", ErrInvalidArgument)
	}
	if node.Capacity < 0 || node.Used < 0 {
		return fmt.Errorf("%w: node capacity and used space cannot be negative", ErrInvalidArgument)
	}
	copyNode := cloneNodeInfo(node)
	state.nodes[copyNode.ID] = &copyNode
	return nil
}

func (r *memoryRepository) GetNode(_ context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return getNode(r.state, nodeID)
}

func (tx *memoryTx) GetNode(_ context.Context, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	return getNode(tx.state, nodeID)
}

// getNode 返回节点信息副本，防止外部修改 labels、lastSeen 等内部字段。
func getNode(state memoryState, nodeID metadata.NodeID) (*metadata.NodeInfo, error) {
	node, ok := state.nodes[nodeID]
	if !ok {
		return nil, fmt.Errorf("%w: node", ErrNotFound)
	}
	copyNode := cloneNodeInfo(*node)
	return &copyNode, nil
}

func (r *memoryRepository) ListNodes(_ context.Context, filter NodeFilter) ([]metadata.NodeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listNodes(r.state, filter)
}

func (tx *memoryTx) ListNodes(_ context.Context, filter NodeFilter) ([]metadata.NodeInfo, error) {
	return listNodes(tx.state, filter)
}

// listNodes 支持按 ID、健康状态、机架、可用区和 labels 进行筛选。
func listNodes(state memoryState, filter NodeFilter) ([]metadata.NodeInfo, error) {
	nodes := make([]metadata.NodeInfo, 0)
	for _, node := range state.nodes {
		if len(filter.IDs) > 0 && !slices.Contains(filter.IDs, node.ID) {
			continue
		}
		if filter.HealthyOnly && !node.Healthy {
			continue
		}
		if filter.Zone != "" && node.Zone != filter.Zone {
			continue
		}
		if filter.Rack != "" && node.Rack != filter.Rack {
			continue
		}
		if !hasLabels(node.Labels, filter.Labels) {
			continue
		}
		nodes = append(nodes, cloneNodeInfo(*node))
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return applyListWindow(nodes, filter.Limit, filter.Offset), nil
}

func (r *memoryRepository) UpdateNodeHeartbeat(_ context.Context, heartbeat NodeHeartbeatPatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateNodeHeartbeat(&r.state, heartbeat)
}

func (tx *memoryTx) UpdateNodeHeartbeat(_ context.Context, heartbeat NodeHeartbeatPatch) error {
	return updateNodeHeartbeat(&tx.state, heartbeat)
}

// updateNodeHeartbeat 聚焦节点的健康状态和容量使用情况更新。
func updateNodeHeartbeat(state *memoryState, heartbeat NodeHeartbeatPatch) error {
	node, ok := state.nodes[heartbeat.NodeID]
	if !ok {
		return fmt.Errorf("%w: node", ErrNotFound)
	}
	if heartbeat.Capacity < 0 || heartbeat.Used < 0 {
		return fmt.Errorf("%w: node capacity and used space cannot be negative", ErrInvalidArgument)
	}
	node.Healthy = heartbeat.Healthy
	node.Capacity = heartbeat.Capacity
	node.Used = heartbeat.Used
	node.LastSeenAt = timePtr(heartbeat.LastSeenAt)
	node.UpdatedAt = heartbeat.LastSeenAt
	return nil
}
