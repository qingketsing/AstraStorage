package integration

import (
	"strings"
	"time"

	"multi_driver/internal/core/cluster"
	"multi_driver/internal/core/consensus"
	"multi_driver/internal/core/consensus/raft"
	"multi_driver/internal/core/replication"
	"multi_driver/internal/db"
	"multi_driver/internal/middleware"
)

// Node 表示一个完整的分布式节点，整合了 cluster 和 raft
type Node struct {
	ID      string
	Address string

	// Cluster 层
	Discovery  *cluster.DiscoveryManager
	Membership *cluster.MembershipManager

	// Consensus 层
	Raft        *raft.Raft
	Coordinator *consensus.LeaderCoordinator

	// Postgres Sql连接
	DB *db.DBConnection

	// 文件复制管理器
	ReplicationMgr *replication.ReplicationManager

	// 中间件管理器（自动监控 Leader 并管理连接）
	RedisManager    *middleware.RedisManager
	RabbitMQManager *middleware.RabbitMQManager

	// RPC 通信
	rpcServer *cluster.RaftRPCServer
	peers     []raft.Peer
}

// NewNode 创建一个完整的节点
// me: 当前节点在整个集群中的索引（0-based）
// peerAddresses: 其他节点的地址列表（不包括自己）
func NewNode(id, address string, me int, peerAddresses []string, persister raft.Persister, applyCh chan raft.ApplyMsg, dbDSN string, redisAddr string, rabbitmqURL string) (*Node, error) {
	node := &Node{
		ID:      id,
		Address: address,
	}

	// 1. 初始化 Discovery
	node.Discovery = cluster.NewDiscoveryManager(id, address, "")

	// 2. 创建 Raft peers（连接到其他节点）
	node.peers = make([]raft.Peer, len(peerAddresses))
	for i, addr := range peerAddresses {
		node.peers[i] = cluster.NewRaftRPC(addr)
	}

	// 3. 初始化 Raft
	node.Raft = raft.Make(node.peers, me, persister, applyCh)

	// 4. 创建 RaftRPC 服务器包装器
	rpcWrapper := &RaftRPCWrapper{raft: node.Raft}
	node.rpcServer = cluster.NewRaftRPCServer(rpcWrapper)

	// 5. 将 Raft RPC 服务器注册到 Discovery
	node.Discovery.SetRaftRPCServer(node.rpcServer)

	// 6. 初始化 Membership
	node.Membership = cluster.NewMembershipManager(id, node.Discovery)

	// 7. 初始化 Leader Coordinator
	node.Coordinator = consensus.NewLeaderCoordinator(node.Raft, node.Membership)

	// 8. PostgreSQL 连接
	dbConn, err := db.NewDBConnection(dbDSN)
	if err != nil {
		return nil, err
	}
	node.DB = dbConn

	// 9. 初始化文件复制管理器（传入数据库连接）
	node.ReplicationMgr = replication.NewReplicationManager(id, node.Membership, dbConn, "FileStorage")
	// 启动复制接收服务
	if err := node.ReplicationMgr.StartReplicationServer("19001"); err != nil {
		return nil, err
	}
	// 启动元数据接收服务
	if err := node.ReplicationMgr.StartMetadataServer("19002"); err != nil {
		return nil, err
	}
	// 启动删除接收服务
	if err := node.ReplicationMgr.StartDeleteServer("19003"); err != nil {
		return nil, err
	}
	// 启动更新接收服务
	if err := node.ReplicationMgr.StartUpdateServer("19004"); err != nil {
		return nil, err
	}

	// 10. 启动服务
	if err := node.Discovery.Start(); err != nil {
		return nil, err
	}

	// 10.5. 初始化已知的peer节点到Membership中（预先添加节点信息，避免等待心跳发现）
	// 从peerAddresses提取节点ID并添加到Membership
	for _, addr := range peerAddresses {
		// 假设peerAddresses格式为"node-X:port"，从地址中提取节点ID
		// 例如: "node-0:29001" → "node-0"
		var peerID string
		if idx := strings.Index(addr, ":"); idx > 0 {
			peerID = addr[:idx]
		} else {
			peerID = addr // 如果没有端口，整个地址就是ID
		}

		// 创建初始节点信息并添加到Discovery
		peerNode := &cluster.Node{
			ID:       peerID,
			Address:  addr,
			Status:   "alive", // 假设初始状态为alive
			LastSeen: time.Now(),
		}
		node.Discovery.AddInitialNode(peerNode)
	}

	// 11. 初始化中间件管理器（自动监控 Leader 并管理连接）
	if redisAddr != "" {
		node.RedisManager = middleware.NewRedisManager(id, redisAddr, node.Raft)
	}
	if rabbitmqURL != "" {
		node.RabbitMQManager = middleware.NewRabbitMQManager(id, rabbitmqURL, node.Raft)
	}

	return node, nil
}

// RaftRPCWrapper 包装 Raft 实例以实现 RaftInterface
type RaftRPCWrapper struct {
	raft *raft.Raft
}

func (w *RaftRPCWrapper) RequestVote(args *cluster.RequestVoteArgs, reply *cluster.RequestVoteReply) {
	// 调用 Raft 的 RequestVote 方法
	rArgs := &raft.RequestVoteArgs{
		Term:         args.Term,
		CandidateId:  args.CandidateId,
		LastLogIndex: args.LastLogIndex,
		LastLogTerm:  args.LastLogTerm,
	}
	rReply := &raft.RequestVoteReply{}

	w.raft.RequestVote(rArgs, rReply)

	reply.Term = rReply.Term
	reply.VoteGranted = rReply.VoteGranted
}

func (w *RaftRPCWrapper) AppendEntries(args *cluster.AppendEntriesArgs, reply *cluster.AppendEntriesReply) {
	// 转换日志条目
	entries := make([]raft.LogEntry, len(args.Entries))
	for i, e := range args.Entries {
		entries[i] = raft.LogEntry{
			Index:   e.Index,
			Term:    e.Term,
			Command: e.Command,
		}
	}

	rArgs := &raft.AppendEntryArgs{
		Term:         args.Term,
		LeaderId:     args.LeaderId,
		PrevLogIndex: args.PrevLogIndex,
		PrevLogTerm:  args.PrevLogTerm,
		Entries:      entries,
		LeaderCommit: args.LeaderCommit,
	}
	rReply := &raft.AppendEntryReply{}

	w.raft.AppendEntries(rArgs, rReply)

	reply.Term = rReply.Term
	reply.Success = rReply.Success
	reply.ConflictIndex = rReply.ConflictIndex
	reply.ConflictTerm = rReply.ConflictTerm
}

// Stop 停止节点
func (node *Node) Stop() {
	node.Discovery.Stop()
	node.Raft.Kill()
	if node.DB != nil {
		node.DB.Close()
	}
	if node.RedisManager != nil {
		node.RedisManager.Stop()
	}
	if node.RabbitMQManager != nil {
		node.RabbitMQManager.Stop()
	}
}

// IsLeader 检查当前节点是否为 Leader
func (node *Node) IsLeader() bool {
	_, isLeader := node.Raft.GetState()
	return isLeader
}

// GetTerm 获取当前任期
func (node *Node) GetTerm() int {
	term, _ := node.Raft.GetState()
	return term
}

// Start 向 Raft 提交命令
func (node *Node) Start(command interface{}) (int, int, bool) {
	return node.Raft.Start(command)
}
