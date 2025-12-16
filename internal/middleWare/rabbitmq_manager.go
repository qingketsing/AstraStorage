package middleware

import (
	"log"
	"sync"
	"time"
)

// RabbitMQManager 管理 RabbitMQ 连接，只在节点为 Leader 时建立连接
type RabbitMQManager struct {
	nodeID     string
	url        string
	raft       LeaderChecker
	client     *RabbitMQ
	mu         sync.Mutex
	stopCh     chan struct{}
	lastLeader bool
}

// NewRabbitMQManager 创建 RabbitMQ 管理器
func NewRabbitMQManager(nodeID string, url string, raft LeaderChecker) *RabbitMQManager {
	manager := &RabbitMQManager{
		nodeID: nodeID,
		url:    url,
		raft:   raft,
		stopCh: make(chan struct{}),
	}

	go manager.watchLeadership()
	return manager
}

// watchLeadership 监控 Leader 状态并自动管理连接
func (m *RabbitMQManager) watchLeadership() {
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

// connect 建立 RabbitMQ 连接
func (m *RabbitMQManager) connect() {
	if m.client != nil {
		return
	}

	client, err := NewRabbitMQConnection(m.url)
	if err != nil {
		log.Printf("[%s] RabbitMQ 连接失败: %v", m.nodeID, err)
		return
	}

	m.client = client
	log.Printf("[%s] 成为 Leader，RabbitMQ 连接成功: %s", m.nodeID, m.url)
}

// disconnect 断开 RabbitMQ 连接
func (m *RabbitMQManager) disconnect() {
	if m.client == nil {
		return
	}

	m.client.Close()
	m.client = nil
	log.Printf("[%s] 失去 Leader 身份，RabbitMQ 连接已断开", m.nodeID)
}

// GetClient 获取 RabbitMQ 客户端（仅 Leader 可用）
func (m *RabbitMQManager) GetClient() *RabbitMQ {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client
}

// Stop 停止管理器
func (m *RabbitMQManager) Stop() {
	close(m.stopCh)
}
