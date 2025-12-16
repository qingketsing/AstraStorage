package middleware

import (
	"log"
	"sync"
	"time"
)

// LeaderChecker 接口用于检查当前节点是否为 Leader
type LeaderChecker interface {
	GetState() (int, bool)
}

// RedisManager 管理 Redis 连接，只在节点为 Leader 时建立连接
type RedisManager struct {
	nodeID     string
	addr       string
	raft       LeaderChecker
	client     *Redis
	mu         sync.Mutex
	stopCh     chan struct{}
	lastLeader bool
}

// NewRedisManager 创建 Redis 管理器
func NewRedisManager(nodeID string, addr string, raft LeaderChecker) *RedisManager {
	manager := &RedisManager{
		nodeID: nodeID,
		addr:   addr,
		raft:   raft,
		stopCh: make(chan struct{}),
	}

	go manager.watchLeadership()
	return manager
}

// watchLeadership 监控 Leader 状态并自动管理连接
func (m *RedisManager) watchLeadership() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			m.disconnect()
			return
		case <-ticker.C:
			_, isLeader := m.raft.GetState()

			m.mu.Lock()
			if isLeader != m.lastLeader {
				if isLeader {
					m.connect()
				} else {
					m.disconnect()
				}
				m.lastLeader = isLeader
			}
			m.mu.Unlock()
		}
	}
}

// connect 建立 Redis 连接
func (m *RedisManager) connect() {
	if m.client != nil {
		return
	}

	client, err := NewRedisConnection(m.addr)
	if err != nil {
		log.Printf("[%s] Redis 连接失败: %v", m.nodeID, err)
		return
	}

	m.client = client
	log.Printf("[%s] 成为 Leader，Redis 连接成功: %s", m.nodeID, m.addr)
}

// disconnect 断开 Redis 连接
func (m *RedisManager) disconnect() {
	if m.client == nil {
		return
	}

	m.client.Close()
	m.client = nil
	log.Printf("[%s] 失去 Leader 身份，Redis 连接已断开", m.nodeID)
}

// GetClient 获取 Redis 客户端（仅 Leader 可用）
func (m *RedisManager) GetClient() *Redis {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client
}

// Stop 停止管理器
func (m *RedisManager) Stop() {
	close(m.stopCh)
}
