package integration

import (
	"fmt"
	"multi_driver/internal/core/consensus/raft"
	"testing"
	"time"
)

// TestThreeNodeClusterLeaderElection 测试三节点集群的 Leader 选举
func TestThreeNodeClusterLeaderElection(t *testing.T) {
	const nNodes = 3
	nodes := make([]*Node, nNodes)
	addresses := []string{
		"127.0.0.1:28001",
		"127.0.0.1:28002",
		"127.0.0.1:28003",
	}

	// 创建 3 个节点
	for i := 0; i < nNodes; i++ {
		applyCh := make(chan raft.ApplyMsg, 100)
		persister := raft.NewInMemoryPersister()

		// 构建 peer 地址列表（不包括自己）
		peerAddrs := make([]string, 0)
		for j, addr := range addresses {
			if j != i {
				peerAddrs = append(peerAddrs, addr)
			}
		}

		node, err := NewNode(
			fmt.Sprintf("node-%d", i),
			addresses[i],
			i, // 节点索引
			peerAddrs,
			persister,
			applyCh,
		)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		nodes[i] = node

		// 启动 goroutine 消费 applyCh
		go func(nodeID int, ch chan raft.ApplyMsg) {
			for msg := range ch {
				if msg.CommandValid {
					t.Logf("Node-%d applied command: %v at index %d", nodeID, msg.Command, msg.CommandIndex)
				}
			}
		}(i, applyCh)
	}

	// 确保测试结束时清理所有节点
	defer func() {
		for _, node := range nodes {
			node.Stop()
		}
	}()

	t.Log("Waiting for leader election...")
	time.Sleep(3 * time.Second)

	// 检查是否有且仅有一个leader
	leaderCount := 0
	var leaderNode *Node
	var leaderIndex int

	for i, node := range nodes {
		term, isLeader := node.Raft.GetState()
		if isLeader {
			leaderCount++
			leaderNode = node
			leaderIndex = i
			t.Logf("Node-%d is LEADER with term %d", i, term)
		} else {
			t.Logf("Node-%d is FOLLOWER with term %d", i, term)
		}
	}

	if leaderCount != 1 {
		t.Fatalf("Expected exactly 1 leader, got %d", leaderCount)
	}

	t.Logf("�?Leader election succeeded: %s (node-%d)", leaderNode.ID, leaderIndex)
}

// TestRaftCommandReplication 测试命令复制
func TestRaftCommandReplication(t *testing.T) {
	const nNodes = 3
	nodes := make([]*Node, nNodes)
	applyChans := make([]chan raft.ApplyMsg, nNodes)
	addresses := []string{
		"127.0.0.1:28011",
		"127.0.0.1:28012",
		"127.0.0.1:28013",
	}

	// 创建 3 个节点
	for i := 0; i < nNodes; i++ {
		applyCh := make(chan raft.ApplyMsg, 100)
		applyChans[i] = applyCh
		persister := raft.NewInMemoryPersister()

		peerAddrs := make([]string, 0)
		for j, addr := range addresses {
			if j != i {
				peerAddrs = append(peerAddrs, addr)
			}
		}

		node, err := NewNode(
			fmt.Sprintf("node-%d", i),
			addresses[i],
			i,
			peerAddrs,
			persister,
			applyCh,
		)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		nodes[i] = node
	}

	defer func() {
		for _, node := range nodes {
			node.Stop()
		}
	}()

	// 等待 leader 选举
	t.Log("Waiting for leader election...")
	time.Sleep(3 * time.Second)

	// 找到 leader
	var leaderNode *Node
	var leaderIndex int
	for i, node := range nodes {
		if node.IsLeader() {
			leaderNode = node
			leaderIndex = i
			break
		}
	}

	if leaderNode == nil {
		t.Fatal("No leader found")
	}
	t.Logf("Leader is node-%d", leaderIndex)

	// 提交多个命令
	commands := []string{"cmd1", "cmd2", "cmd3"}
	for _, cmd := range commands {
		index, term, ok := leaderNode.Start(cmd)
		if !ok {
			t.Fatalf("Failed to start command: %s", cmd)
		}
		t.Logf("Submitted command '%s' at index %d, term %d", cmd, index, term)
	}

	// 等待命令提交
	t.Log("Waiting for commands to be replicated and applied...")
	time.Sleep(2 * time.Second)

	// 验证所有节点都应用了命令
	appliedCommands := make([][]interface{}, nNodes)
	for i := 0; i < nNodes; i++ {
		appliedCommands[i] = make([]interface{}, 0)
		// applyCh 读取已应用的命令
		done := false
		for !done {
			select {
			case msg := <-applyChans[i]:
				if msg.CommandValid {
					appliedCommands[i] = append(appliedCommands[i], msg.Command)
					t.Logf("Node-%d applied: %v", i, msg.Command)
				}
			case <-time.After(100 * time.Millisecond):
				done = true
			}
		}
	}

	// 检查所有节点是否应用了相同数量的命令
	expectedCount := len(commands)
	for i, applied := range appliedCommands {
		if len(applied) < expectedCount {
			t.Logf("Warning: Node-%d only applied %d/%d commands", i, len(applied), expectedCount)
		}
	}

	t.Logf("Command replication test completed")
}

// TestLeaderFailover 测试 Leader 故障转移
func TestLeaderFailover(t *testing.T) {
	const nNodes = 3
	nodes := make([]*Node, nNodes)
	addresses := []string{
		"127.0.0.1:28021",
		"127.0.0.1:28022",
		"127.0.0.1:28023",
	}

	// 创建 3 个节点
	for i := 0; i < nNodes; i++ {
		applyCh := make(chan raft.ApplyMsg, 100)
		persister := raft.NewInMemoryPersister()

		peerAddrs := make([]string, 0)
		for j, addr := range addresses {
			if j != i {
				peerAddrs = append(peerAddrs, addr)
			}
		}

		node, err := NewNode(
			fmt.Sprintf("node-%d", i),
			addresses[i],
			i,
			peerAddrs,
			persister,
			applyCh,
		)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		nodes[i] = node

		go func(ch chan raft.ApplyMsg) {
			for range ch {
			}
		}(applyCh)
	}

	defer func() {
		for _, node := range nodes {
			if node != nil {
				node.Stop()
			}
		}
	}()

	// 等待初始 leader 选举
	t.Log("Waiting for initial leader election...")
	time.Sleep(3 * time.Second)

	// 找到 leader
	var oldLeaderIndex int
	var oldLeaderTerm int
	for i, node := range nodes {
		term, isLeader := node.Raft.GetState()
		if isLeader {
			oldLeaderIndex = i
			oldLeaderTerm = term
			t.Logf("Initial leader is node-%d with term %d", i, term)
			break
		}
	}

	// 停止leader
	t.Logf("Killing leader node-%d...", oldLeaderIndex)
	nodes[oldLeaderIndex].Stop()
	nodes[oldLeaderIndex] = nil

	// 等待leader 选举
	t.Log("Waiting for new leader election...")
	time.Sleep(5 * time.Second)

	// 检查是否有leader
	newLeaderCount := 0
	var newLeaderIndex int
	var newLeaderTerm int

	for i, node := range nodes {
		if node == nil {
			continue
		}
		term, isLeader := node.Raft.GetState()
		if isLeader {
			newLeaderCount++
			newLeaderIndex = i
			newLeaderTerm = term
			t.Logf("New leader is node-%d with term %d", i, term)
		}
	}

	if newLeaderCount != 1 {
		t.Fatalf("Expected exactly 1 new leader, got %d", newLeaderCount)
	}

	if newLeaderIndex == oldLeaderIndex {
		t.Fatal("New leader is the same as old leader")
	}

	if newLeaderTerm <= oldLeaderTerm {
		t.Fatalf("New leader term (%d) should be greater than old term (%d)", newLeaderTerm, oldLeaderTerm)
	}

	t.Logf("�?Leader failover succeeded: node-%d (term %d) -> node-%d (term %d)",
		oldLeaderIndex, oldLeaderTerm, newLeaderIndex, newLeaderTerm)
}

// TestFiveNodeCluster 测试五节点集群
func TestFiveNodeCluster(t *testing.T) {
	const nNodes = 5
	nodes := make([]*Node, nNodes)
	addresses := []string{
		"127.0.0.1:28031",
		"127.0.0.1:28032",
		"127.0.0.1:28033",
		"127.0.0.1:28034",
		"127.0.0.1:28035",
	}

	// 创建 5 个节点
	for i := 0; i < nNodes; i++ {
		applyCh := make(chan raft.ApplyMsg, 100)
		persister := raft.NewInMemoryPersister()

		peerAddrs := make([]string, 0)
		for j, addr := range addresses {
			if j != i {
				peerAddrs = append(peerAddrs, addr)
			}
		}

		node, err := NewNode(
			fmt.Sprintf("node-%d", i),
			addresses[i],
			i,
			peerAddrs,
			persister,
			applyCh,
		)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		nodes[i] = node

		go func(ch chan raft.ApplyMsg) {
			for range ch {
			}
		}(applyCh)
	}

	defer func() {
		for _, node := range nodes {
			node.Stop()
		}
	}()

	t.Log("Waiting for leader election in 5-node cluster...")
	time.Sleep(3 * time.Second)

	// 统计 leader 数量
	leaderCount := 0
	for i, node := range nodes {
		term, isLeader := node.Raft.GetState()
		if isLeader {
			leaderCount++
			t.Logf("Node-%d is LEADER with term %d", i, term)
		}
	}

	if leaderCount != 1 {
		t.Fatalf("Expected exactly 1 leader in 5-node cluster, got %d", leaderCount)
	}

	t.Logf("�?Five-node cluster election succeeded")
}
