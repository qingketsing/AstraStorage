// 节点发现器部分
// *************************************************************************************
// 1. 维护节点视图（Node），可以记录ID、地址、角色、存活状态与资源指标。
// 2. 提供 HTTP 接口：/join 注册新节点，/heartbeat 接收心跳并更新资源状态；可挂载Raft RPC处理器。
// 3. 周期性广播心跳（sendHeartbeat）并做健康检查（checkNodes），超时节点会被移除并通过通道通知上层。
// 4. 对外暴露加入/离开事件通道，供应用层调度使用。
// *************************************************************************************
//	主要结构：
//  	Node：
// 			ID,
// 			地址，
// 			角色，
// 			状态，
// 			最后心跳时间，
// 			所有需要的资源调度信息（CPU，内存，磁盘，上传下载活跃数，带宽占用）
//		DiscoveryManager:
// 			维护自身信息，
// 			已知的节点表，
// 			心跳检查，
// 			RaftRPC服务器挂载
//
// 	启动&停止：
//		Start()：并行启动 HTTP 服务、心跳广播循环、健康检查循环。
//		Stop()：关闭 stopCh，终止后台循环。
//
// 	HTTP 路由：
// 		POST /join：新节点注册。解码 Node，标记存活、更新时间戳，写入 nodes，通过 joinedCh 通知。
// 		POST /heartbeat：心跳上报。解码 HeartbeatPayload，更新已知节点的存活时间、角色与资源字段；未知节点则创建并加入表。
//
// 	心跳机制：
//		broadcastPresence()：定期发送心跳
// 		sendHeartbeat()：
// 			调用 monitor.CollectStats() 生成 HeartbeatPayload。
// 			若自身为 Leader，则向已知 peers 广播；否则只发给指定 Leader。
// 			通过 postHeartbeat 以 HTTP POST 发送。
// 			用已有的 HTTP 服务端口，避免额外的传输层实现，同时易于穿透常见防火墙/负载均衡，避免被拦截。
//
//	存活检查：
// 		healthCheckLoop()：每秒调用 checkNodes()
//  	checkNodes()：
// 			若某节点 LastSeen 超过 nodeTimeout 未更新，则认为超时，移出 nodes，通过 leftCh 通知。
// 			如超时节点角色为 Leader，预留触发选举的入口。
//
//
//
// 	TODO：
// 		接入Raft的leader选举部分
//

package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Node 表示集群中的一个节点
type Node struct {
	ID        string    `json:"id"`
	Address   string    `json:"address"`
	Role      string    `json:"role"`      // "leader", "follower", "candidate"
	Status    string    `json:"status"`    // "alive", "dead"
	LastSeen  time.Time `json:"-"`         // 本地记录的最后一次可见时间，不序列化传输
	Heartbeat int64     `json:"heartbeat"` // 心跳计数器

	// 资源状态 (从 HeartbeatPayload 同步)
	CPUUsage        float64 `json:"cpu_usage"`
	MemoryUsage     uint64  `json:"memory_usage"`
	DiskTotal       uint64  `json:"disk_total"` // 磁盘总空间
	DiskFree        uint64  `json:"disk_free"`
	ActiveUploads   int     `json:"active_uploads"`
	ActiveDownloads int     `json:"active_downloads"`
	BandwidthUsed   float64 `json:"bandwidth_used"`
}

// DiscoveryManager 节点发现管理器
type DiscoveryManager struct {
	selfID     string
	selfAddr   string
	leaderAddr string // 初始配置的 Leader 地址，如果是 Leader 自己则为空或忽略
	nodes      map[string]*Node
	mu         sync.RWMutex
	stopCh     chan struct{}
	joinedCh   chan *Node
	leftCh     chan string

	monitor *HeartbeatMonitor // 引入心跳监视器

	// 配置项
	heartbeatInterval time.Duration
	nodeTimeout       time.Duration

	// Raft RPC 服务器
	raftRPCServer *RaftRPCServer
}

// NewDiscoveryManager 创建一个新的发现管理器
func NewDiscoveryManager(id, addr, leaderAddr string) *DiscoveryManager {
	return &DiscoveryManager{
		selfID:            id,
		selfAddr:          addr,
		leaderAddr:        leaderAddr,
		nodes:             make(map[string]*Node),
		stopCh:            make(chan struct{}),
		joinedCh:          make(chan *Node, 10),
		leftCh:            make(chan string, 10),
		monitor:           NewHeartbeatMonitor(id, addr, "follower"), // 默认初始化为 follower
		heartbeatInterval: 2 * time.Second,                           // 每2秒发送一次心跳
		nodeTimeout:       10 * time.Second,                          // 10秒无心跳视为超时
	}
}

// Start 启动节点发现
func (dm *DiscoveryManager) Start() error {
	// 1. 启动HTTP服务用于接收节点注册和心跳
	go dm.startHTTPServer()

	// 2. 定期广播自己的存在（发送心跳）
	go dm.broadcastPresence()

	// 3. 定期检查节点健康状态
	go dm.healthCheckLoop()

	return nil
}

// Stop 停止服务
func (dm *DiscoveryManager) Stop() {
	close(dm.stopCh)
}

// AddInitialNode 添加初始节点信息（用于启动时预先添加已知节点）
func (dm *DiscoveryManager) AddInitialNode(node *Node) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// 添加到nodes map
	dm.nodes[node.ID] = node

	// 通知Membership Manager
	select {
	case dm.joinedCh <- node:
	default:
		// 如果channel满了，记录日志但不阻塞
		log.Printf("[%s] Warning: joinedCh full when adding initial node %s", dm.selfID, node.ID)
	}

	log.Printf("[%s] Added initial peer node: %s at %s", dm.selfID, node.ID, node.Address)
}

// startHTTPServer 启动 HTTP 服务器处理节点请求
func (dm *DiscoveryManager) startHTTPServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/join", dm.handleJoin)
	mux.HandleFunc("/heartbeat", dm.handleHeartbeat)

	// 注册 Raft RPC 处理器
	if dm.raftRPCServer != nil {
		dm.raftRPCServer.RegisterHandlers(mux)
	}

	// 注意:实际生产中这里应该解析 selfAddr 中的端口,或者使用独立的内部通信端口
	// 这里假设 selfAddr 格式为 "ip:port"
	server := &http.Server{
		Addr:           dm.selfAddr,
		Handler:        mux,
		ReadTimeout:    5 * time.Second,
		WriteTimeout:   5 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	log.Printf("[%s] Discovery server listening on %s", dm.selfID, dm.selfAddr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()
}

// handleJoin 处理新节点加入请求
func (dm *DiscoveryManager) handleJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var node Node
	if err := json.NewDecoder(r.Body).Decode(&node); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	node.LastSeen = time.Now()
	node.Status = "alive"

	dm.mu.Lock()
	dm.nodes[node.ID] = &node
	dm.mu.Unlock()

	log.Printf("[%s] Node joined: %s (%s)", dm.selfID, node.ID, node.Address)

	// 通知上层应用有新节点加入
	select {
	case dm.joinedCh <- &node:
	default:
	}

	w.WriteHeader(http.StatusOK)
}

// handleHeartbeat 处理心跳请求
func (dm *DiscoveryManager) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload HeartbeatPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid body", http.StatusBadRequest)
		return
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if node, exists := dm.nodes[payload.NodeID]; exists {
		node.LastSeen = time.Now()
		node.Heartbeat = payload.Timestamp
		node.Status = "alive"
		node.Role = payload.Role

		// 更新资源状态
		node.CPUUsage = payload.CPUUsage
		node.MemoryUsage = payload.MemoryUsage
		node.DiskTotal = payload.DiskTotal
		node.DiskFree = payload.DiskFree
		node.ActiveUploads = payload.ActiveUploads
		node.ActiveDownloads = payload.ActiveDownloads
		node.BandwidthUsed = payload.BandwidthUsed
	} else {
		// 如果是未知的节点发来心跳，视作新加入
		newNode := &Node{
			ID:              payload.NodeID,
			Address:         payload.Address,
			Role:            payload.Role,
			Status:          "alive",
			LastSeen:        time.Now(),
			Heartbeat:       payload.Timestamp,
			CPUUsage:        payload.CPUUsage,
			MemoryUsage:     payload.MemoryUsage,
			DiskTotal:       payload.DiskTotal,
			DiskFree:        payload.DiskFree,
			ActiveUploads:   payload.ActiveUploads,
			ActiveDownloads: payload.ActiveDownloads,
			BandwidthUsed:   payload.BandwidthUsed,
		}
		dm.nodes[payload.NodeID] = newNode
		log.Printf("[%s] Unknown node heartbeat, adding: %s (Disk: %d/%d)", dm.selfID, payload.NodeID, payload.DiskFree, payload.DiskTotal)
	}
}

// broadcastPresence 定期发送心跳
func (dm *DiscoveryManager) broadcastPresence() {
	ticker := time.NewTicker(dm.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-dm.stopCh:
			return
		case <-ticker.C:
			dm.sendHeartbeat()
		}
	}
}

// sendHeartbeat 发送心跳逻辑
func (dm *DiscoveryManager) sendHeartbeat() {
	// 构建心跳包
	payload := dm.monitor.CollectStats()

	// 如果我是 Follower，我主要需要向 Leader 发送心跳
	// 如果我是 Leader，我需要向所有 Followers 发送心跳
	// 简化起见：这里演示向配置的 Leader 地址发送注册/心跳
	// 在完全分布式的 P2P 中，这里会遍历 dm.nodes 发送

	target := dm.leaderAddr
	if target == "" || target == dm.selfAddr {
		// 如果没有配置 Leader 或者自己就是 Leader，则向已知的 peers 广播
		dm.mu.RLock()
		peers := make([]string, 0, len(dm.nodes))
		for _, n := range dm.nodes {
			if n.Address != dm.selfAddr {
				peers = append(peers, n.Address)
			}
		}
		dm.mu.RUnlock()

		for _, addr := range peers {
			go dm.postHeartbeat(addr, payload)
		}
	} else {
		// 向特定 Leader 发送
		go dm.postHeartbeat(target, payload)
	}
}

func (dm *DiscoveryManager) postHeartbeat(addr string, payload HeartbeatPayload) {
	data, _ := json.Marshal(payload)
	resp, err := http.Post(fmt.Sprintf("http://%s/heartbeat", addr), "application/json", bytes.NewBuffer(data))
	if err != nil {
		// log.Printf("Failed to send heartbeat to %s: %v", addr, err)
		return
	}
	defer resp.Body.Close()
}

// healthCheckLoop 定期检查节点是否超时
func (dm *DiscoveryManager) healthCheckLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dm.stopCh:
			return
		case <-ticker.C:
			dm.checkNodes()
		}
	}
}

func (dm *DiscoveryManager) checkNodes() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	now := time.Now()
	for id, node := range dm.nodes {
		if now.Sub(node.LastSeen) > dm.nodeTimeout {
			log.Printf("[%s] Node %s timed out (last seen: %v)", dm.selfID, id, node.LastSeen)
			delete(dm.nodes, id)

			// 通知节点离开
			select {
			case dm.leftCh <- id:
			default:
			}

			// TODO: 如果超时的节点是 Leader，这里需要触发选举逻辑
			if node.Role == "leader" {
				log.Printf("!!! LEADER %s IS DEAD. INITIATING ELECTION !!!", id)
				// triggerElection()
			}
		}
	}
}

// SetRaftRPCServer 设置 Raft RPC 服务器
func (dm *DiscoveryManager) SetRaftRPCServer(rpcServer *RaftRPCServer) {
	dm.raftRPCServer = rpcServer
}

// GetHeartbeatMetrics 获取心跳监视器的指标数据
func (dm *DiscoveryManager) GetHeartbeatMetrics() HeartbeatPayload {
	return dm.monitor.CollectStats()
}
