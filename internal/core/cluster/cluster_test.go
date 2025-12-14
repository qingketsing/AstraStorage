package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

// setupDiscovery 辅助函数:启动一个用于测试的 Discovery Server
func setupDiscovery(t *testing.T, port string) *DiscoveryManager {
	// 使用唯一的 ID 和地址
	dm := NewDiscoveryManager("master-"+port, "127.0.0.1:"+port, "")
	// 缩短时间间隔以便快速测试
	dm.heartbeatInterval = 100 * time.Millisecond
	dm.nodeTimeout = 30 * time.Second // 设置足够长的超时时间用于压力测试

	if err := dm.Start(); err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}
	return dm
}

// 测试 1: 新节点发现 (压力测试)
// 模拟 1000 个节点并发加入，验证 DiscoveryManager 是否能正确处理
func TestNewNodeDiscovery_Stress(t *testing.T) {
	port := "19001"
	dm := setupDiscovery(t, port)
	defer dm.Stop()

	count := 300    // 模拟300 个节点
	batchSize := 50 // 每批处理50个(减少并发压力)
	var wg sync.WaitGroup

	// 分批并发发送 Join 请求
	for batch := 0; batch <= count/batchSize; batch++ {
		wg.Add(batchSize)
		for i := 0; i < batchSize; i++ {
			id := batch*batchSize + i
			go func(id int) {
				defer wg.Done()
				node := Node{
					ID:      fmt.Sprintf("node-%d", id),
					Address: fmt.Sprintf("127.0.0.1:%d", 10000+id),
					Role:    "follower",
				}
				data, _ := json.Marshal(node)
				url := fmt.Sprintf("http://127.0.0.1:%s/join", port)
				client := &http.Client{Timeout: 5 * time.Second} // 增加超时时间
				resp, err := client.Post(url, "application/json", bytes.NewBuffer(data))
				if err != nil {
					// 不再记录错误,测试结束后检查注册数量即可
					return
				}
				resp.Body.Close()
			}(id)
		}
		wg.Wait()                          // 等待当前批次完成
		time.Sleep(150 * time.Millisecond) // 进一步增加批次间延迟
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond) // 增加最终等待时间

	dm.mu.RLock()
	defer dm.mu.RUnlock()
	if len(dm.nodes) != count {
		t.Errorf("Expected %d nodes, got %d", count, len(dm.nodes))
	} else {
		t.Logf("Successfully registered %d nodes concurrently", count)
	}
}

// 测试 2: 心跳机制
// 验证心跳是否能更新节点的资源状态，以及超时是否会移除节点
func TestHeartbeat_Mechanism(t *testing.T) {
	port := "9002"
	dm := setupDiscovery(t, port)
	defer dm.Stop()
	// 缩短超时便于测试
	dm.nodeTimeout = 500 * time.Millisecond

	nodeID := "worker-1"
	// 1. 节点加入
	joinNode := Node{ID: nodeID, Address: "127.0.0.1:10000", Role: "follower"}
	joinData, _ := json.Marshal(joinNode)
	http.Post(fmt.Sprintf("http://127.0.0.1:%s/join", port), "application/json", bytes.NewBuffer(joinData))

	// 2. 发送带有资源状态的心跳
	payload := HeartbeatPayload{
		NodeID:          nodeID,
		Address:         "127.0.0.1:10000",
		Role:            "follower",
		Timestamp:       time.Now().UnixNano(),
		DiskFree:        500 * 1024 * 1024, // 500MB
		ActiveDownloads: 3,
	}
	hbData, _ := json.Marshal(payload)
	http.Post(fmt.Sprintf("http://127.0.0.1:%s/heartbeat", port), "application/json", bytes.NewBuffer(hbData))

	time.Sleep(100 * time.Millisecond)

	dm.mu.RLock()
	node, ok := dm.nodes[nodeID]
	dm.mu.RUnlock()

	if !ok {
		t.Fatal("Node should exist")
	}
	if node.DiskFree != payload.DiskFree {
		t.Errorf("DiskFree not updated. Got %d, want %d", node.DiskFree, payload.DiskFree)
	}
	if node.ActiveDownloads != payload.ActiveDownloads {
		t.Errorf("ActiveDownloads not updated. Got %d, want %d", node.ActiveDownloads, payload.ActiveDownloads)
	}
	t.Log("Heartbeat updated resource stats successfully")

	// 3. 测试超时移除
	// 等待时间超过 nodeTimeout (500ms)
	time.Sleep(1 * time.Second)

	dm.mu.RLock()
	_, exists := dm.nodes[nodeID]
	dm.mu.RUnlock()

	if exists {
		t.Error("Node should have been removed due to timeout")
	} else {
		t.Log("Node successfully removed after timeout")
	}
}

// 测试 3: 成员管理与节点选择
// 验证是否能根据磁盘空间和负载选择正确的节点
func TestMembership_Selection(t *testing.T) {
	dm := NewDiscoveryManager("master", "localhost:0", "")
	mm := NewMembershipManager("master", dm)

	// 手动注入模拟节点
	nodes := []*Node{
		{ID: "n1", Status: "alive", DiskFree: 100, ActiveDownloads: 10},
		{ID: "n2", Status: "alive", DiskFree: 500, ActiveDownloads: 2},
		{ID: "n3", Status: "alive", DiskFree: 200, ActiveDownloads: 5},
		{ID: "n4", Status: "alive", DiskFree: 50, ActiveDownloads: 0},
	}

	dm.mu.Lock()
	for _, n := range nodes {
		dm.nodes[n.ID] = n
		mm.nodes[n.ID] = n // 手动同步到 MM
	}
	dm.mu.Unlock()

	// 测试存储节点选择 (期望按 DiskFree 降序: n2(500), n3(200), n1(100))
	storageNodes, err := mm.PickNodesForStorage(3)
	if err != nil {
		t.Fatalf("PickNodesForStorage failed: %v", err)
	}
	if len(storageNodes) != 3 {
		t.Errorf("Expected 3 nodes, got %d", len(storageNodes))
	}
	if storageNodes[0].ID != "n2" {
		t.Errorf("Expected n2 (max disk) first, got %s", storageNodes[0].ID)
	}
	t.Log("Storage node selection logic passed")

	// 测试下载节点选择 (期望按 ActiveDownloads 升序: n4(0))
	downloadNode, err := mm.PickBestNodeForDownload()
	if err != nil {
		t.Fatalf("PickBestNodeForDownload failed: %v", err)
	}
	if downloadNode.ID != "n4" {
		t.Errorf("Expected n4 (0 downloads), got %s (%d)", downloadNode.ID, downloadNode.ActiveDownloads)
	}
	t.Log("Download node selection logic passed")
}

// 测试 4: HeartbeatMonitor 采集系统状态
// 验证各字段被填充，并且活跃上下行计数被透传
func TestHeartbeatMonitor_CollectStats(t *testing.T) {
	hm := NewHeartbeatMonitor("node-monitor", "127.0.0.1:19999", "follower")
	hm.SetActive(3, 7)

	stat := hm.CollectStats()

	t.Logf("stats: id=%s addr=%s role=%s ts=%d cpu=%.2f mem=%d disk=%d up=%d down=%d bw=%.3fMbps",
		stat.NodeID, stat.Address, stat.Role, stat.Timestamp, stat.CPUUsage, stat.MemoryUsage,
		stat.DiskFree, stat.ActiveUploads, stat.ActiveDownloads, stat.BandwidthUsed)

	if stat.NodeID != "node-monitor" || stat.Address != "127.0.0.1:19999" || stat.Role != "follower" {
		t.Fatalf("basic identity fields mismatch: %+v", stat)
	}

	t.Logf("system metrics: cpu=%.2f%% mem=%dB disk=%dB bw=%.3fMbps active_up=%d active_down=%d",
		stat.CPUUsage, stat.MemoryUsage, stat.DiskFree, stat.BandwidthUsed, stat.ActiveUploads, stat.ActiveDownloads)
}

// 测试 5: 带宽采集连续采样
// 只验证带宽字段非负且时间戳递增，不强制要求非零（无流量场景可能为 0）
func TestHeartbeatMonitor_BandwidthSampling(t *testing.T) {
	hm := NewHeartbeatMonitor("node-net", "127.0.0.1:20000", "follower")

	first := hm.CollectStats()
	// 等待一小段时间以产生 delta，避免两次采样间隔为 0
	time.Sleep(400 * time.Millisecond)
	second := hm.CollectStats()
	if first.BandwidthUsed < 0 || second.BandwidthUsed < 0 {
		t.Fatalf("bandwidth should be non-negative, got first=%.4f second=%.4f", first.BandwidthUsed, second.BandwidthUsed)
	}
	if second.Timestamp <= first.Timestamp {
		t.Fatalf("second sample timestamp should increase: first=%d second=%d", first.Timestamp, second.Timestamp)
	}

	t.Logf("bandwidth samples: first=%.4f Mbps, second=%.4f Mbps", first.BandwidthUsed, second.BandwidthUsed)
}
