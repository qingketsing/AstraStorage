package consensus

import (
	"errors"
	"log"
	"sync"
	"time"

	"multi_driver/internal/core/cluster"
	"multi_driver/internal/core/consensus/raft"
)

// LeaderCoordinator 负责协调 Leader 节点的特有业务
// 它充当 Raft 共识层、集群成员管理和上层业务逻辑之间的胶水代码
type LeaderCoordinator struct {
	mu         sync.RWMutex
	raft       *raft.Raft
	membership *cluster.MembershipManager
	isLeader   bool
}

// NewLeaderCoordinator 创建协调器
func NewLeaderCoordinator(rf *raft.Raft, mm *cluster.MembershipManager) *LeaderCoordinator {
	lc := &LeaderCoordinator{
		raft:       rf,
		membership: mm,
		isLeader:   false,
	}
	// 启动后台协程监控 Raft 状态
	go lc.monitorState()
	return lc
}

// monitorState 定期检查 Raft 状态，感知 Leadership 变化
func (lc *LeaderCoordinator) monitorState() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		term, isLeader := lc.raft.GetState()

		lc.mu.Lock()
		if isLeader != lc.isLeader {
			if isLeader {
				log.Printf("Promoted to Leader at term %d", term)
				// TODO: 这里可以触发 Leader 上任后的初始化操作，例如：
				// 1. 从 PostgreSQL 加载文件树到 Redis
				// 2. 恢复未完成的复制任务
			} else {
				log.Printf("Stepped down from Leader at term %d", term)
			}
			lc.isLeader = isLeader
		}
		lc.mu.Unlock()
	}
}

// EnsureLeader 检查当前节点是否为 Leader
func (lc *LeaderCoordinator) EnsureLeader() error {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	if !lc.isLeader {
		return errors.New("not leader")
	}
	return nil
}

// PlanUpload 为上传请求分配存储节点
// 1. 检查 Leader 身份
// 2. 通过 Membership 选择 3 个最佳副本节点 (基于磁盘空间)
// 3. 通过 Raft 记录文件元数据（预提交，保证一致性）
func (lc *LeaderCoordinator) PlanUpload(filename string, size int64) ([]*cluster.Node, error) {
	if err := lc.EnsureLeader(); err != nil {
		return nil, err
	}

	// 1. 选择 3 个副本节点
	nodes, err := lc.membership.PickNodesForStorage(3)
	if err != nil {
		return nil, err
	}

	// 2. 构造操作日志并通过 Raft 达成共识
	// 实际项目中，这里应该定义一个结构体，例如 Op{Type:"PrepareUpload", ...}
	op := struct {
		Op       string
		Filename string
		Size     int64
	}{
		Op:       "PrepareUpload",
		Filename: filename,
		Size:     size,
	}

	_, _, isLeader := lc.raft.Start(op)
	if !isLeader {
		return nil, errors.New("lost leadership during consensus")
	}

	// 注意：在强一致性系统中，这里应该等待 Raft ApplyCh 的确认（通过 channel 通知）
	// 只有 Raft 确认日志已提交，才返回给客户端。
	// 为了简化，这里假设 Start 成功即开始流程。

	return nodes, nil
}

// PlanDownload 为下载请求选择最佳节点
// 1. 检查 Leader 身份
// 2. 查询文件所在的节点列表 (需要元数据存储支持)
// 3. 从候选节点中选择网络/负载最好的一个
func (lc *LeaderCoordinator) PlanDownload(filename string) (*cluster.Node, error) {
	if err := lc.EnsureLeader(); err != nil {
		return nil, err
	}

	// TODO: 实际逻辑应该是：
	// 1. 从 Redis/DB 获取该文件实际存储在哪些节点 (candidateNodeIDs)
	// 2. candidates := lc.membership.GetNodes(candidateNodeIDs)
	// 3. bestNode := lc.membership.PickBestNodeFrom(candidates)

	// 目前简化为：直接让 Membership 选一个全局最快的 (假设所有节点都有该文件)
	bestNode, err := lc.membership.PickBestNodeForDownload()
	if err != nil {
		return nil, err
	}

	return bestNode, nil
}
