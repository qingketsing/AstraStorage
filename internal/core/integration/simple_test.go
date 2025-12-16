package integration

import (
	"fmt"
	"multi_driver/internal/core/consensus/raft"
	"testing"
	"time"
)

// TestSimpleThreeNodeRaft 最简单的三节点 Raft 测试
func TestSimpleThreeNodeRaft(t *testing.T) {
	// 创建 3 个节点的地址
	addresses := []string{
		"127.0.0.1:29001",
		"127.0.0.1:29002",
		"127.0.0.1:29003",
	}

	// 创建节点
	nodes := make([]*Node, 3)
	for i := 0; i < 3; i++ {
		applyCh := make(chan raft.ApplyMsg, 100)
		persister := raft.NewInMemoryPersister()

		// 其他节点地址
		var peerAddrs []string
		for j := 0; j < 3; j++ {
			if j != i {
				peerAddrs = append(peerAddrs, addresses[j])
			}
		}

		node, err := NewNode(
			fmt.Sprintf("node-%d", i),
			addresses[i],
			i,
			peerAddrs,
			persister,
			applyCh,
			"", // dbDSN - 测试中不需要数据库
			"", // redisAddr
			"", // rabbitmqURL
		)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		nodes[i] = node

		t.Logf("Created node-%d at %s with peers: %v", i, addresses[i], peerAddrs)

		// 消费 applyCh
		go func(id int, ch chan raft.ApplyMsg) {
			for msg := range ch {
				if msg.CommandValid {
					t.Logf("Node-%d applied command: %v", id, msg.Command)
				}
			}
		}(i, applyCh)
	}

	defer func() {
		for _, node := range nodes {
			node.Stop()
		}
	}()

	// 等待选举
	t.Log("Waiting 5 seconds for leader election...")
	time.Sleep(5 * time.Second)

	// 检查状态
	leaderCount := 0
	for i, node := range nodes {
		term, isLeader := node.Raft.GetState()
		status := "FOLLOWER"
		if isLeader {
			status = "LEADER"
			leaderCount++
		}
		t.Logf("Node-%d: %s, Term=%d", i, status, term)
	}

	if leaderCount == 0 {
		t.Error("No leader elected")
	} else if leaderCount > 1 {
		t.Errorf("Multiple leaders: %d", leaderCount)
	} else {
		t.Log("✓ Exactly one leader elected")
	}
}
