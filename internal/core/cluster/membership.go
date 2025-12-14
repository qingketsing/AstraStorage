// 成员管理
// *************************************************************************************
//	维护本地节点表，封装调度策略
// *************************************************************************************
//	主要结构：
// 		MemebershipManager:
// 			nodes：存储所有节点
// 			leaderID：当前leader的ID
// 			selfID：自己的ID
//			discovery：事件源
//
// 	功能函数：
// 		GetLeader()：读取当前缓存的 Leader 节点；若未设置或不在 nodes 中，返回 error。
// 		PickNodesForStorage(count)：
// 			仅选取 Status == "alive" 的节点。
// 			按 DiskFree 降序排序，截取前 count 个。
// 			若总节点数不足 count，返回错误；若存活候选少于 count，返回全部候选。
// 			注意：不会自动排除自身节点；如需要可在调用侧过滤。
// 		GetAllNodes()：返回当前节点表的浅拷贝切片。

package cluster

import (
	"errors"
	"sort"
	"sync"
)

// MembershipManager 负责维护集群成员的高级视图，并提供节点选择策略
type MembershipManager struct {
	mu        sync.RWMutex
	nodes     map[string]*Node // 存储id string对应所有节点
	leaderID  string           // 当前 Leader 的 ID
	selfID    string           // 自己的 ID
	discovery *DiscoveryManager
}

// NewMembershipManager 创建成员管理器
func NewMembershipManager(selfID string, dm *DiscoveryManager) *MembershipManager {
	mm := &MembershipManager{
		nodes:     make(map[string]*Node),
		selfID:    selfID,
		discovery: dm,
	}

	// 启动协程监听 Discovery 的变化
	go mm.watchDiscovery()

	return mm
}

// watchDiscovery 监听底层发现服务的事件，保持本地成员列表最新
func (mm *MembershipManager) watchDiscovery() {
	for {
		select {
		case node := <-mm.discovery.joinedCh:
			mm.mu.Lock()
			mm.nodes[node.ID] = node
			mm.mu.Unlock()
		case nodeID := <-mm.discovery.leftCh:
			mm.mu.Lock()
			delete(mm.nodes, nodeID)
			if mm.leaderID == nodeID {
				mm.leaderID = "" // Leader 挂了
				// 注意：这里只是更新本地视图，真正的选举逻辑通常由 Raft 层触发
			}
			mm.mu.Unlock()
		}
	}
}

// GetLeader 获取当前 Leader 节点
func (mm *MembershipManager) GetLeader() (*Node, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if mm.leaderID == "" {
		return nil, errors.New("no leader found")
	}

	if node, ok := mm.nodes[mm.leaderID]; ok {
		return node, nil
	}
	return nil, errors.New("leader node not found in registry")
}

// SetLeader 更新 Leader (通常由 Raft 选举回调调用)
func (mm *MembershipManager) SetLeader(leaderID string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.leaderID = leaderID
}

// PickNodesForStorage 选择 N 个最适合存储文件的节点
// 策略：优先选择磁盘空间最大的节点，且排除自己（如果需要）
// 用于满足"一个文件要有三个备份"的需求
func (mm *MembershipManager) PickNodesForStorage(count int) ([]*Node, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	if len(mm.nodes) < count {
		return nil, errors.New("not enough nodes in cluster")
	}

	// 复制一份节点列表用于排序
	candidates := make([]*Node, 0, len(mm.nodes))
	for _, n := range mm.nodes {
		if n.Status == "alive" {
			candidates = append(candidates, n)
		}
	}

	// 调度策略：按剩余磁盘空间降序排序 (DiskFree 越大越好)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].DiskFree > candidates[j].DiskFree
	})

	if len(candidates) < count {
		return candidates, nil // 返回所有可用节点
	}
	return candidates[:count], nil
}

// PickBestNodeForDownload 为下载请求选择最佳节点
// 策略：选择负载最低（活跃下载数最少）或带宽占用最低的节点
// 用于满足"自动找到下载速度最快的服务器"的需求
func (mm *MembershipManager) PickBestNodeForDownload() (*Node, error) {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	var bestNode *Node
	var minLoad float64 = -1.0

	for _, node := range mm.nodes {
		if node.Status != "alive" {
			continue
		}

		// 计算负载分数。这里简单用 ActiveDownloads，也可以结合 BandwidthUsed
		// 分数越低越好
		load := float64(node.ActiveDownloads)

		// 如果需要更复杂的逻辑，可以结合带宽：
		// load = float64(node.ActiveDownloads) * 0.7 + node.BandwidthUsed * 0.3

		if minLoad == -1.0 || load < minLoad {
			minLoad = load
			bestNode = node
		}
	}

	if bestNode == nil {
		return nil, errors.New("no available nodes for download")
	}

	return bestNode, nil
}

// GetAllNodes 返回所有节点列表
func (mm *MembershipManager) GetAllNodes() []*Node {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	nodes := make([]*Node, 0, len(mm.nodes))
	for _, n := range mm.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}
