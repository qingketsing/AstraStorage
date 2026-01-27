// 心跳
// *************************************************************************************
// 处理心跳信息收集
// *************************************************************************************
//	主要结构：
//		HeartbeatPayload：
//	 		包含了主机信息

package cluster

import (
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// HeartbeatPayload 定义了心跳包中携带的详细状态信息
// 包含负载均衡和调度所需的数据
type HeartbeatPayload struct {
	NodeID    string `json:"node_id"`
	Address   string `json:"address"`
	Role      string `json:"role"`
	Timestamp int64  `json:"timestamp"`

	// 资源状态
	CPUUsage        float64 `json:"cpu_usage"`        // CPU 使用率百分比
	MemoryUsage     uint64  `json:"memory_usage"`     // 内存使用量 (bytes)
	DiskTotal       uint64  `json:"disk_total"`       // 磁盘总空间 (bytes) - 固定配额
	DiskFree        uint64  `json:"disk_free"`        // 磁盘剩余空间 (bytes)
	ActiveUploads   int     `json:"active_uploads"`   // 当前正在进行的上传任务数
	ActiveDownloads int     `json:"active_downloads"` // 当前正在进行的下载任务数

	// 网络状态 (用于选择下载速度最快的节点)
	BandwidthUsed float64 `json:"bandwidth_used"` // 当前带宽使用情况 (Mbps)
}

// HeartbeatMonitor 负责检查并且记录本地状态然后生成心跳包
type HeartbeatMonitor struct {
	nodeID   string
	address  string
	role     string
	iface    string // 可选：指定网卡名
	diskPath string // 可选：指定磁盘根路径，Linux 可用 /data 挂载盘

	mu              sync.Mutex
	activeUploads   int
	activeDownloads int
	lastNetIO       net.IOCountersStat
	lastNetAt       time.Time
	lastBandwidth   float64 // 上次有效带宽值（Mbps）
}

// 可在外部初始化活跃数，用于测试
func (hm *HeartbeatMonitor) SetActive(u, d int) {
	hm.mu.Lock()
	hm.activeUploads = u
	hm.activeDownloads = d
	hm.mu.Unlock()
}

// 可选指定采样网卡名，如 "Ethernet", "eth0" 等
func (hm *HeartbeatMonitor) SetInterface(name string) {
	hm.mu.Lock()
	hm.iface = name
	hm.mu.Unlock()
}

// 可选指定磁盘路径，Linux 可设置为 /data 或具体挂载点
func (hm *HeartbeatMonitor) SetDiskPath(path string) {
	hm.mu.Lock()
	hm.diskPath = path
	hm.mu.Unlock()
}

func NewHeartbeatMonitor(nodeID, address, role string) *HeartbeatMonitor {
	hm := &HeartbeatMonitor{
		nodeID:  nodeID,
		address: address,
		role:    role,
	}
	// 初始化网卡统计
	hm.initNetStats()
	return hm
}

func (hm *HeartbeatMonitor) initNetStats() {
	if hm.iface != "" {
		if ios, _ := net.IOCounters(true); len(ios) > 0 {
			for _, io := range ios {
				if io.Name == hm.iface {
					hm.lastNetIO = io
					hm.lastNetAt = time.Now()
					return
				}
			}
		}
	}
	if ios, _ := net.IOCounters(false); len(ios) > 0 {
		hm.lastNetIO = ios[0]
		hm.lastNetAt = time.Now()
	}
}

// CollectStats 收集当前节点的系统状态
func (hm *HeartbeatMonitor) CollectStats() HeartbeatPayload {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// CPU 使用率（短周期即时值）
	cpuPercent := 0.0
	if vals, err := cpu.Percent(0, false); err == nil && len(vals) > 0 {
		cpuPercent = vals[0]
	}

	// 系统内存占用
	memUsed := uint64(0)
	if vm, err := mem.VirtualMemory(); err == nil {
		memUsed = vm.Used
	}

	// 磁盘剩余空间
	// 需求：强制限制每个节点可用存储空间为 50GB
	const NodeStorageQuota = 50 * 1024 * 1024 * 1024 // 50GB
	diskTotal := uint64(NodeStorageQuota)
	diskFree := uint64(0)

	root := hm.diskRoot()
	if du, err := disk.Usage(root); err == nil {
		// du.Used 是整个分区的已用空间，包括操作系统和其他文件
		// 但由于我们在 Docker 中，或者没有更好的隔离方法，我们假设 Used 就是我们要计算的
		// 更好的方法是统计实际数据目录的大小，但性能开销大。
		// 这里采用简化的配额逻辑：
		// Free = Quota - Used (如果小于0则为0)
		// 注意：如果节点上还有其他大文件，这会吃掉 Quota。

		// 修正：在 Docker 容器 OverlayFS 中，du.Used 反映的是底层文件系统的使用情况
		// 如果宿主机的磁盘快满了，这里也会反映出来。
		// 我们的目标是 "固定可用 50GB"，即表现得像一个 50GB 的盘。

		// 逻辑：
		// 1. 获取物理已用空间 du.Used
		// 2. 如果 du.Used > Quota, 则 Free = 0
		// 3. 否则 Free = Quota - du.Used
		// (这假设我们是从 0 开始用的，或者愿意把已有数据计入 Quota)

		used := du.Used
		if used >= NodeStorageQuota {
			diskFree = 0
		} else {
			diskFree = NodeStorageQuota - used
		}
	} else {
		// 获取失败，保守设为 0 或者全部
		// 设为 0 以避免被调度
		diskFree = 0
	}

	// 带宽使用（Mbps）= (Δbytes * 8) / Δ秒 / 1e6
	bwMbps := 0.0
	if io, ok := hm.sampleNetIO(); ok {
		now := time.Now()
		hm.mu.Lock()
		// 最小采样间隔 200ms，过短则沿用上次值
		if !hm.lastNetAt.IsZero() {
			dt := now.Sub(hm.lastNetAt).Seconds()
			if dt >= 0.2 {
				dbytes := float64((io.BytesSent + io.BytesRecv) - (hm.lastNetIO.BytesSent + hm.lastNetIO.BytesRecv))
				if dbytes > 0 && dt > 0 {
					bwMbps = (dbytes * 8.0) / dt / 1e6
					hm.lastBandwidth = bwMbps
				} else {
					bwMbps = hm.lastBandwidth
				}
			} else {
				bwMbps = hm.lastBandwidth
			}
		}
		hm.lastNetIO = io
		hm.lastNetAt = now
		activeUp := hm.activeUploads
		activeDown := hm.activeDownloads
		hm.mu.Unlock()

		return HeartbeatPayload{
			NodeID:          hm.nodeID,
			Address:         hm.address,
			Role:            hm.role,
			Timestamp:       now.UnixNano(),
			CPUUsage:        cpuPercent,
			MemoryUsage:     memUsed,
			DiskTotal:       diskTotal,
			DiskFree:        diskFree,
			ActiveUploads:   activeUp,
			ActiveDownloads: activeDown,
			BandwidthUsed:   bwMbps,
		}
	}

	// 若带宽采样失败，仍返回其他指标
	hm.mu.Lock()
	activeUp := hm.activeUploads
	activeDown := hm.activeDownloads
	hm.mu.Unlock()

	return HeartbeatPayload{
		NodeID:          hm.nodeID,
		Address:         hm.address,
		Role:            hm.role,
		Timestamp:       time.Now().UnixNano(),
		CPUUsage:        cpuPercent,
		MemoryUsage:     memUsed,
		DiskTotal:       diskTotal,
		DiskFree:        diskFree,
		ActiveUploads:   activeUp,
		ActiveDownloads: activeDown,
		BandwidthUsed:   bwMbps,
	}
}

// diskRoot 返回用于统计的磁盘路径，优先使用用户配置
func (hm *HeartbeatMonitor) diskRoot() string {
	hm.mu.Lock()
	path := hm.diskPath
	hm.mu.Unlock()

	if path != "" {
		return path
	}

	if runtime.GOOS == "windows" {
		return `C:\\`
	}
	// Linux/Unix 默认根分区，可按需通过 SetDiskPath 设置为 /data 等挂载点
	return "/"
}

// sampleNetIO 根据配置选择网卡：优先指定 iface；否则尝试汇总；若失败则返回 false
func (hm *HeartbeatMonitor) sampleNetIO() (net.IOCountersStat, bool) {
	// 优先使用指定网卡
	hm.mu.Lock()
	iface := hm.iface
	hm.mu.Unlock()

	if iface != "" {
		if ios, err := net.IOCounters(true); err == nil {
			for _, io := range ios {
				if io.Name == iface {
					return io, true
				}
			}
		}
	}

	// 尝试汇总
	if ios, err := net.IOCounters(false); err == nil && len(ios) > 0 {
		return ios[0], true
	}

	// 最后尝试列举非 loopback 的第一个网卡
	if ios, err := net.IOCounters(true); err == nil {
		for _, io := range ios {
			nameLower := strings.ToLower(io.Name)
			if strings.HasPrefix(nameLower, "lo") || strings.Contains(nameLower, "loopback") {
				continue
			}
			return io, true
		}
	}

	return net.IOCountersStat{}, false
}
