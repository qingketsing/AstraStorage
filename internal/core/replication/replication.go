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

// BroadcastMetadata 向所有其他节点广播文件元数据（不传输文件内容）
// storageNodes 是实际存储文件的节点列表
// 这样所有节点的数据库都会有文件记录，即使它们不存储实际文件
func (rm *ReplicationManager) BroadcastMetadata(fileName string, fileSize int64, storageNodes []string, ownerID string) error {
	// 获取所有节点（包括自己）
	allNodes := rm.membership.GetAllNodes()

	if len(allNodes) == 0 {
		log.Printf("[%s] no nodes available for metadata broadcast", rm.nodeID)
		return nil
	}

	storageNodesStr := strings.Join(storageNodes, ",")

	// 向所有节点（除了自己和已存储文件的节点）发送元数据
	successCount := 0
	for _, node := range allNodes {
		if node.ID == rm.nodeID {
			continue // 跳过自己，自己已经保存过了
		}

		// 跳过已经通过文件复制获得元数据的节点
		isStorageNode := false
		for _, sn := range storageNodes {
			if sn == node.ID {
				isStorageNode = true
				break
			}
		}
		if isStorageNode {
			continue
		}

		log.Printf("[%s] broadcasting metadata to node %s (%s)", rm.nodeID, node.ID, node.Address)

		err := rm.sendMetadataToNode(node, fileName, fileSize, storageNodesStr, ownerID)
		if err != nil {
			log.Printf("[%s] broadcast metadata to node %s failed: %v", rm.nodeID, node.ID, err)
			continue
		}

		successCount++
		log.Printf("[%s] successfully broadcast metadata to node %s", rm.nodeID, node.ID)
	}

	log.Printf("[%s] metadata broadcast completed: %d/%d nodes", rm.nodeID, successCount, len(allNodes)-len(storageNodes)-1)
	return nil
}

// sendMetadataToNode 向指定节点发送文件元数据（不包含文件内容）
func (rm *ReplicationManager) sendMetadataToNode(node *cluster.Node, fileName string, fileSize int64, storageNodes string, ownerID string) error {
	// 解析节点地址
	host, _, err := net.SplitHostPort(node.Address)
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到节点的元数据服务端口（使用不同的端口避免与文件复制冲突）
	metadataAddr := net.JoinHostPort(host, "19002") // 元数据服务端口

	conn, err := net.DialTimeout("tcp", metadataAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	// 设置超时
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 发送元数据协议头：命令\n文件名\n文件大小\n存储节点列表\n所有者ID\n
	header := fmt.Sprintf("METADATA\n%s\n%d\n%s\n%s\n", fileName, fileSize, storageNodes, ownerID)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send metadata header failed: %w", err)
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

// StartMetadataServer 启动元数据接收服务
func (rm *ReplicationManager) StartMetadataServer(listenPort string) error {
	if listenPort == "" {
		listenPort = "19002" // 默认端口
	}

	listener, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		return fmt.Errorf("start metadata server failed: %w", err)
	}

	log.Printf("[%s] metadata server started on port %s", rm.nodeID, listenPort)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("[%s] accept metadata connection failed: %v", rm.nodeID, err)
				continue
			}
			go rm.handleMetadataConnection(conn)
		}
	}()

	return nil
}

// handleMetadataConnection 处理元数据接收连接
func (rm *ReplicationManager) handleMetadataConnection(conn net.Conn) {
	defer conn.Close()

	// 设置超时
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 读取协议头
	header := make([]byte, 1024)
	n, err := conn.Read(header)
	if err != nil {
		log.Printf("[%s] read metadata header failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	lines := strings.Split(string(header[:n]), "\n")
	if len(lines) < 2 {
		log.Printf("[%s] invalid metadata header", rm.nodeID)
		conn.Write([]byte("ER"))
		return
	}

	command := lines[0]

	// 处理删除元数据命令
	if command == "DELETE_META" {
		fileName := lines[1]
		log.Printf("[%s] received delete metadata request for file: %s", rm.nodeID, fileName)

		// 从数据库删除元数据
		if rm.db != nil {
			if err := rm.db.DeleteFileByName(fileName); err != nil {
				log.Printf("[%s] delete metadata from database failed: %v", rm.nodeID, err)
				// 即使失败也返回 OK，因为文件可能已经不存在
			} else {
				log.Printf("[%s] deleted metadata from database: %s", rm.nodeID, fileName)
			}
		}

		conn.Write([]byte("OK"))
		return
	}

	// 处理更新元数据命令：UPDATE_META\n文件名\n文件大小\n存储节点列表\n
	if command == "UPDATE_META" {
		if len(lines) < 4 {
			log.Printf("[%s] invalid update metadata header", rm.nodeID)
			conn.Write([]byte("ER"))
			return
		}

		fileName := lines[1]
		var fileSize int64
		fmt.Sscanf(lines[2], "%d", &fileSize)
		storageNodes := lines[3]

		log.Printf("[%s] received update metadata request for file: %s, size=%d, nodes=%s",
			rm.nodeID, fileName, fileSize, storageNodes)

		// 更新数据库中的元数据
		if rm.db != nil {
			query := `UPDATE files SET file_size = $1, storage_nodes = $2, updated_at = NOW() WHERE file_name = $3`
			if _, err := rm.db.Exec(query, fileSize, storageNodes, fileName); err != nil {
				log.Printf("[%s] update metadata in database failed: %v", rm.nodeID, err)
				conn.Write([]byte("ER"))
				return
			}
			log.Printf("[%s] updated metadata in database: %s", rm.nodeID, fileName)
		}

		conn.Write([]byte("OK"))
		return
	}

	// 处理正常的元数据同步命令：METADATA\n文件名\n文件大小\n存储节点列表\n所有者ID\n
	if command != "METADATA" || len(lines) < 5 {
		log.Printf("[%s] invalid metadata command", rm.nodeID)
		conn.Write([]byte("ER"))
		return
	}

	fileName := lines[1]
	var fileSize int64
	fmt.Sscanf(lines[2], "%d", &fileSize)
	storageNodes := lines[3]
	ownerID := lines[4]

	log.Printf("[%s] receiving file metadata: %s, size=%d, storage_nodes=%s, owner=%s",
		rm.nodeID, fileName, fileSize, storageNodes, ownerID)

	// 保存元数据到数据库（注意：本地没有文件，所以 local_path 为空）
	if rm.db != nil {
		err = rm.saveMetadataToDatabase(fileName, fileSize, storageNodes, ownerID)
		if err != nil {
			log.Printf("[%s] save metadata to database failed: %v", rm.nodeID, err)
			conn.Write([]byte("ER"))
			return
		}
		log.Printf("[%s] metadata saved to database: name=%s, size=%d", rm.nodeID, fileName, fileSize)
	}

	conn.Write([]byte("OK"))
}

// saveMetadataToDatabase 保存元数据到本地数据库（不包含本地文件路径）
func (rm *ReplicationManager) saveMetadataToDatabase(fileName string, fileSize int64, storageNodes string, ownerID string) error {
	if rm.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	// 创建文件记录，local_path 为空表示本节点没有存储文件
	fileInfo := db.FileUploadInfo{
		FileName:     fileName,
		FileSize:     fileSize,
		LocalPath:    "", // 空字符串表示本节点没有存储文件
		StorageNodes: storageNodes,
		CreatedAt:    time.Now(),
		OwnerID:      ownerID,
	}

	_, err := rm.db.SaveFileUpload(fileInfo)
	return err
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

// === 文件删除功能 ===

// DeleteFile 删除文件（包括本地文件和所有副本）
// 返回成功删除的节点列表
func (rm *ReplicationManager) DeleteFile(fileName string) ([]string, error) {
	// 1. 查询文件信息
	fileInfo, err := rm.db.GetFileByName(fileName)
	if err != nil {
		return nil, fmt.Errorf("query file failed: %w", err)
	}

	// 2. 解析存储节点列表
	storageNodes := strings.Split(fileInfo.StorageNodes, ",")
	log.Printf("[%s] deleting file %s from nodes: %v", rm.nodeID, fileName, storageNodes)

	// 3. 向所有存储节点发送删除请求
	successNodes := make([]string, 0)
	for _, nodeID := range storageNodes {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}

		if nodeID == rm.nodeID {
			// 删除本地文件
			if fileInfo.LocalPath != "" {
				if err := os.Remove(fileInfo.LocalPath); err != nil {
					log.Printf("[%s] delete local file failed: %v", rm.nodeID, err)
				} else {
					log.Printf("[%s] deleted local file: %s", rm.nodeID, fileInfo.LocalPath)
					successNodes = append(successNodes, rm.nodeID)
				}
			}
		} else {
			// 向其他节点发送删除请求
			var node *cluster.Node
			for _, n := range rm.membership.GetAllNodes() {
				if n.ID == nodeID {
					node = n
					break
				}
			}

			if node == nil {
				log.Printf("[%s] node %s not found in membership", rm.nodeID, nodeID)
				continue
			}

			if err := rm.deleteFileFromNode(node, fileName); err != nil {
				log.Printf("[%s] delete file from node %s failed: %v", rm.nodeID, nodeID, err)
			} else {
				log.Printf("[%s] successfully deleted file from node %s", rm.nodeID, nodeID)
				successNodes = append(successNodes, nodeID)
			}
		}
	}

	if len(successNodes) == 0 {
		return successNodes, fmt.Errorf("failed to delete file from any node")
	}

	return successNodes, nil
}

// deleteFileFromNode 向指定节点发送删除文件请求
func (rm *ReplicationManager) deleteFileFromNode(node *cluster.Node, fileName string) error {
	// 解析节点地址
	host, _, err := net.SplitHostPort(node.Address)
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到节点的删除服务端口
	deleteAddr := net.JoinHostPort(host, "19003") // 固定的删除服务端口

	conn, err := net.DialTimeout("tcp", deleteAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	// 设置超时
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 发送删除协议头：DELETE\n文件名\n
	header := fmt.Sprintf("DELETE\n%s\n", fileName)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send delete request failed: %w", err)
	}

	// 等待响应
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

// StartDeleteServer 启动删除接收服务
func (rm *ReplicationManager) StartDeleteServer(listenPort string) error {
	if listenPort == "" {
		listenPort = "19003" // 默认端口
	}

	listener, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		return fmt.Errorf("start delete server failed: %w", err)
	}

	log.Printf("[%s] delete server started on port %s", rm.nodeID, listenPort)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("[%s] accept delete connection failed: %v", rm.nodeID, err)
				continue
			}
			go rm.handleDeleteConnection(conn)
		}
	}()

	return nil
}

// handleDeleteConnection 处理删除请求连接
func (rm *ReplicationManager) handleDeleteConnection(conn net.Conn) {
	defer conn.Close()

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// 读取协议头
	header := make([]byte, 1024)
	n, err := conn.Read(header)
	if err != nil {
		log.Printf("[%s] read delete header failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	// 解析协议：DELETE\n文件名\n
	lines := strings.Split(string(header[:n]), "\n")
	if len(lines) < 2 || lines[0] != "DELETE" {
		log.Printf("[%s] invalid delete protocol", rm.nodeID)
		conn.Write([]byte("ER"))
		return
	}

	fileName := lines[1]
	log.Printf("[%s] received delete request for file: %s", rm.nodeID, fileName)

	// 查询文件信息
	fileInfo, err := rm.db.GetFileByName(fileName)
	if err != nil {
		log.Printf("[%s] file not found: %s, error: %v", rm.nodeID, fileName, err)
		conn.Write([]byte("OK")) // 文件不存在也返回成功
		return
	}

	// 删除本地文件（如果存在）
	if fileInfo.LocalPath != "" {
		if err := os.Remove(fileInfo.LocalPath); err != nil {
			log.Printf("[%s] delete local file failed: %v", rm.nodeID, err)
			// 继续删除数据库记录
		} else {
			log.Printf("[%s] deleted local file: %s", rm.nodeID, fileInfo.LocalPath)
		}
	}

	// 删除数据库记录
	if err := rm.db.DeleteFileByName(fileName); err != nil {
		log.Printf("[%s] delete database record failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	log.Printf("[%s] successfully deleted file: %s", rm.nodeID, fileName)
	conn.Write([]byte("OK"))
}

// StartUpdateServer 启动更新接收服务
func (rm *ReplicationManager) StartUpdateServer(listenPort string) error {
	if listenPort == "" {
		listenPort = "19004" // 默认端口
	}

	listener, err := net.Listen("tcp", ":"+listenPort)
	if err != nil {
		return fmt.Errorf("start update server failed: %w", err)
	}

	log.Printf("[%s] update server started on port %s", rm.nodeID, listenPort)

	go func() {
		defer listener.Close()
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("[%s] accept update connection failed: %v", rm.nodeID, err)
				continue
			}
			go rm.handleUpdateConnection(conn)
		}
	}()

	return nil
}

// handleUpdateConnection 处理文件更新请求连接
func (rm *ReplicationManager) handleUpdateConnection(conn net.Conn) {
	defer conn.Close()

	// 设置超时
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// 读取协议头：UPDATE\n文件名\n文件大小\n
	header := make([]byte, 1024)
	n, err := conn.Read(header)
	if err != nil {
		log.Printf("[%s] read update header failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	lines := strings.Split(string(header[:n]), "\n")
	if len(lines) < 3 || lines[0] != "UPDATE" {
		log.Printf("[%s] invalid update protocol", rm.nodeID)
		conn.Write([]byte("ER"))
		return
	}

	fileName := lines[1]
	var fileSize int64
	fmt.Sscanf(lines[2], "%d", &fileSize)

	log.Printf("[%s] receiving file update: %s, size=%d", rm.nodeID, fileName, fileSize)

	// 查询文件信息
	fileInfo, err := rm.db.GetFileByName(fileName)
	if err != nil {
		log.Printf("[%s] file not found: %s, error: %v", rm.nodeID, fileName, err)
		conn.Write([]byte("ER"))
		return
	}

	// 备份旧文件
	if fileInfo.LocalPath != "" {
		oldFilePath := fileInfo.LocalPath + ".old"
		if err := os.Rename(fileInfo.LocalPath, oldFilePath); err != nil {
			log.Printf("[%s] backup old file failed: %v", rm.nodeID, err)
		} else {
			log.Printf("[%s] backed up old file to: %s", rm.nodeID, oldFilePath)
			defer os.Remove(oldFilePath) // 成功后删除备份
		}
	}

	// 确保存储目录存在
	if err := os.MkdirAll(rm.storageDir, 0755); err != nil {
		log.Printf("[%s] create storage dir failed: %v", rm.nodeID, err)
		conn.Write([]byte("ER"))
		return
	}

	// 创建新文件
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

	// 计算已读取的header之后的剩余数据
	headerEnd := len("UPDATE\n") + len(fileName) + 1 + len(lines[2]) + 1
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

	file.Close()

	// 更新数据库中的文件信息
	if rm.db != nil {
		query := `UPDATE files SET file_size = $1, local_path = $2, updated_at = NOW() WHERE file_name = $3`
		if _, err := rm.db.Exec(query, fileSize, localPath, fileName); err != nil {
			log.Printf("[%s] update file in database failed: %v", rm.nodeID, err)
			// 不返回错误，因为文件已经成功接收
		} else {
			log.Printf("[%s] updated file in database: name=%s, size=%d", rm.nodeID, fileName, fileSize)
		}
	}

	log.Printf("[%s] successfully received file update: %s", rm.nodeID, fileName)
	conn.Write([]byte("OK"))
}
