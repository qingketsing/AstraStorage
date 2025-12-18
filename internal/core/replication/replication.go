// 文件复制模块
// 负责将文件从主节点复制到其他节点，实现数据冗余备份

package replication

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"multi_driver/internal/core/cluster"
	"multi_driver/internal/db"
)

const (
	// 默认复制份数（包括原节点）
	DefaultReplicaCount = 2
	// 复制超时时间
	ReplicationTimeout = 30 * time.Second
	// 分块大小
	ChunkSize = 1 << 20 // 1MB
)

// ReplicationManager 复制管理器
type ReplicationManager struct {
	nodeID       string
	membership   *cluster.MembershipManager
	replicaCount int
	storageDir   string           // 本地存储目录
	db           *db.DBConnection // 数据库连接
}

// NewReplicationManager 创建复制管理器
func NewReplicationManager(nodeID string, membership *cluster.MembershipManager, dbConn *db.DBConnection, storageDir string) *ReplicationManager {
	if storageDir == "" {
		storageDir = "FileStorage"
	}
	return &ReplicationManager{
		nodeID:       nodeID,
		membership:   membership,
		replicaCount: DefaultReplicaCount,
		storageDir:   storageDir,
		db:           dbConn,
	}
}

// SetReplicaCount 设置副本数量
func (rm *ReplicationManager) SetReplicaCount(count int) {
	if count > 0 {
		rm.replicaCount = count
	}
}

// ReplicateFile 将文件复制到其他节点
// 返回成功复制的节点ID列表
func (rm *ReplicationManager) ReplicateFile(localPath, fileName string, fileSize int64) ([]string, error) {
	// 1. 选择目标节点（排除自己）
	targetNodes, err := rm.selectTargetNodes()
	if err != nil {
		return nil, fmt.Errorf("select target nodes failed: %w", err)
	}

	if len(targetNodes) == 0 {
		log.Printf("[%s] no other nodes available for replication", rm.nodeID)
		return []string{rm.nodeID}, nil // 只有自己
	}

	// 2. 计算文件校验和
	checksum, err := calculateChecksum(localPath)
	if err != nil {
		return nil, fmt.Errorf("calculate checksum failed: %w", err)
	}

	// 3. 预先构建完整的存储节点列表（包括自己和所有目标节点）
	// 这样所有节点都会记录相同的、完整的节点列表
	allTargetNodes := []string{rm.nodeID}
	for _, node := range targetNodes {
		allTargetNodes = append(allTargetNodes, node.ID)
	}
	completeNodesList := strings.Join(allTargetNodes, ",")

	// 4. 并发复制到各个节点
	successNodes := []string{rm.nodeID} // 包含自己
	for _, node := range targetNodes {
		log.Printf("[%s] replicating file %s to node %s (%s)", rm.nodeID, fileName, node.ID, node.Address)

		err := rm.sendFileToNode(node, localPath, fileName, fileSize, checksum, completeNodesList)
		if err != nil {
			log.Printf("[%s] replicate to node %s failed: %v", rm.nodeID, node.ID, err)
			continue
		}

		successNodes = append(successNodes, node.ID)
		log.Printf("[%s] successfully replicated to node %s", rm.nodeID, node.ID)
	}

	if len(successNodes) == 1 {
		return successNodes, fmt.Errorf("failed to replicate to any node")
	}

	return successNodes, nil
}

// selectTargetNodes 选择复制目标节点
func (rm *ReplicationManager) selectTargetNodes() ([]*cluster.Node, error) {
	// 需要复制的节点数量（不包括自己）
	targetCount := rm.replicaCount - 1
	if targetCount <= 0 {
		return nil, nil
	}

	// 请求足够的节点（replicaCount个），确保过滤掉自己后能有targetCount个节点
	// 为了保证能获取到足够的节点，请求 replicaCount + 1 个，然后过滤自己，取前 targetCount 个
	requestCount := rm.replicaCount + 1
	nodes, err := rm.membership.PickNodesForStorage(requestCount)
	if err != nil {
		return nil, err
	}

	// 过滤掉自己
	targetNodes := make([]*cluster.Node, 0)
	for _, node := range nodes {
		if node.ID != rm.nodeID {
			targetNodes = append(targetNodes, node)
		}
	}

	// 确保只取需要的数量
	if len(targetNodes) > targetCount {
		targetNodes = targetNodes[:targetCount]
	}

	return targetNodes, nil
}

// sendFileToNode 通过TCP将文件发送到指定节点
func (rm *ReplicationManager) sendFileToNode(node *cluster.Node, localPath, fileName string, fileSize int64, checksum string, completeNodesList string) error {
	// 解析节点地址
	host, _, err := net.SplitHostPort(node.Address)
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到节点的复制服务端口（使用节点地址的主机+固定端口）
	replicationAddr := net.JoinHostPort(host, "19001") // 固定的复制服务端口

	conn, err := net.DialTimeout("tcp", replicationAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	// 设置整体超时
	conn.SetDeadline(time.Now().Add(ReplicationTimeout))

	// 发送复制协议头：命令\n文件名\n文件大小\n校验和\n存储节点列表\n
	// 注意：这里直接使用传入的完整节点列表
	header := fmt.Sprintf("REPLICATE\n%s\n%d\n%s\n%s\n", fileName, fileSize, checksum, completeNodesList)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send header failed: %w", err)
	}

	// 打开本地文件
	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file failed: %w", err)
	}
	defer file.Close()

	// 发送文件内容
	buf := make([]byte, ChunkSize)
	sent := int64(0)
	for sent < fileSize {
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("read file failed: %w", err)
		}
		if n == 0 {
			break
		}

		_, err = conn.Write(buf[:n])
		if err != nil {
			return fmt.Errorf("send file data failed: %w", err)
		}
		sent += int64(n)
	}

	if sent != fileSize {
		return fmt.Errorf("incomplete send: expected=%d, sent=%d", fileSize, sent)
	}

	// 等待对方确认
	response := make([]byte, 3)
	n, err := conn.Read(response)
	if err != nil {
		return fmt.Errorf("read response failed: %w", err)
	}

	if n != 2 || string(response[:2]) != "OK" {
		return fmt.Errorf("node returned error: %s", string(response[:n]))
	}

	return nil
}

// StartReplicationServer 启动复制接收服务
func (rm *ReplicationManager) StartReplicationServer(listenPort string) error {
	if listenPort == "" {
		listenPort = "19001" // 默认端口
	}

	listener, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		return fmt.Errorf("start replication server failed: %w", err)
	}

	log.Printf("[%s] replication server started on port %s", rm.nodeID, listenPort)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("[%s] accept replication connection failed: %v", rm.nodeID, err)
				continue
			}
			go rm.handleReplicationConnection(conn)
		}
	}()

	return nil
}

// handleReplicationConnection 处理复制连接
func (rm *ReplicationManager) handleReplicationConnection(conn net.Conn) {
	defer conn.Close()

	// 设置超时
	conn.SetDeadline(time.Now().Add(ReplicationTimeout))

	// 读取协议头：命令\n文件名\n文件大小\n校验和\n存储节点列表\n
	header := make([]byte, 1024)
	n, err := conn.Read(header)
	if err != nil {
		log.Printf("[%s] read replication header failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	lines := strings.Split(string(header[:n]), "\n")
	if len(lines) < 5 || lines[0] != "REPLICATE" {
		log.Printf("[%s] invalid replication header", rm.nodeID)
		conn.Write([]byte("ER"))
		return
	}

	fileName := lines[1]
	var fileSize int64
	fmt.Sscanf(lines[2], "%d", &fileSize)
	expectedChecksum := lines[3]
	storageNodes := lines[4] // 完整的存储节点列表

	log.Printf("[%s] receiving replicated file: %s, size=%d, storage_nodes=%s", rm.nodeID, fileName, fileSize, storageNodes)

	// 确保存储目录存在
	if err := os.MkdirAll(rm.storageDir, 0755); err != nil {
		log.Printf("[%s] create storage dir failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	// 创建本地文件
	localPath := filepath.Join(rm.storageDir, fileName)
	file, err := os.Create(localPath)
	if err != nil {
		log.Printf("[%s] create local file failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}
	defer file.Close()

	// 接收文件内容
	received := int64(0)
	buf := make([]byte, ChunkSize)

	// 计算已读取的header之后的剩余数据（现在包含存储节点列表）
	headerEnd := len("REPLICATE\n") + len(fileName) + 1 + len(lines[2]) + 1 + len(expectedChecksum) + 1 + len(storageNodes) + 1
	if n > headerEnd {
		// header之后还有数据，先写入
		extraData := header[headerEnd:n]
		written, err := file.Write(extraData)
		if err != nil {
			log.Printf("[%s] write file failed: %v", rm.nodeID, err)
			conn.Write([]byte("ER"))
			return
		}
		received += int64(written)
	}

	// 继续接收剩余数据
	for received < fileSize {
		toRead := fileSize - received
		if toRead > int64(len(buf)) {
			toRead = int64(len(buf))
		}

		n, err := conn.Read(buf[:toRead])
		if err != nil && err != io.EOF {
			log.Printf("[%s] read file data failed: %v", rm.nodeID, err)
			conn.Write([]byte("ER"))
			return
		}
		if n == 0 {
			break
		}

		_, err = file.Write(buf[:n])
		if err != nil {
			log.Printf("[%s] write file failed: %v", rm.nodeID, err)
			conn.Write([]byte("ER"))
			return
		}
		received += int64(n)
	}

	if received != fileSize {
		log.Printf("[%s] incomplete receive: expected=%d, received=%d", rm.nodeID, fileSize, received)
		conn.Write([]byte("ER"))
		return
	}

	// 验证校验和
	file.Close()
	actualChecksum, err := calculateChecksum(localPath)
	if err != nil {
		log.Printf("[%s] calculate checksum failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	if actualChecksum != expectedChecksum {
		log.Printf("[%s] checksum mismatch: expected=%s, actual=%s", rm.nodeID, expectedChecksum, actualChecksum)
		os.Remove(localPath)
		conn.Write([]byte("ER"))
		return
	}

	// 保存文件信息到数据库（使用完整的存储节点列表）
	if rm.db != nil {
		err = rm.saveFileToDatabase(fileName, localPath, fileSize, storageNodes)
		if err != nil {
			log.Printf("[%s] save file to database failed: %v", rm.nodeID, err)
			// 注意：这里不返回错误，因为文件已经成功接收
			// 只是数据库记录失败，可以后续补救
		} else {
			log.Printf("[%s] file saved to local database: name=%s, size=%d, storage_nodes=%s", rm.nodeID, fileName, fileSize, storageNodes)
		}
	}

	log.Printf("[%s] successfully received replicated file: %s", rm.nodeID, fileName)
	conn.Write([]byte("OK"))
}

// saveFileToDatabase 保存文件信息到本地数据库
func (rm *ReplicationManager) saveFileToDatabase(fileName, localPath string, fileSize int64, storageNodes string) error {
	if rm.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// 创建文件记录，使用传入的完整存储节点列表
	fileInfo := db.FileUploadInfo{
		FileName:     fileName,
		FileSize:     fileSize,
		LocalPath:    localPath,
		StorageNodes: storageNodes, // 使用完整的节点列表
		CreatedAt:    time.Now(),
	}

	_, err := rm.db.SaveFileUpload(fileInfo)
	return err
}

// calculateChecksum 计算文件的MD5校验和
func calculateChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
