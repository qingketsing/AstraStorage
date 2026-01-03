package update

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"multi_driver/internal/core/integration"
	"multi_driver/internal/db"
)

// UpdateMetaDataArgs 客户端更新文件请求结构
type UpdateMetaDataArgs struct {
	Operation string `json:"operation"` // "update_file"
	ClientIP  string `json:"client_ip"`
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
}

// UpdateMetaDataReply 返回给客户端的响应
type UpdateMetaDataReply struct {
	OK         bool   `json:"ok"`
	Err        string `json:"err,omitempty"`
	UpdateAddr string `json:"update_addr"` // 更新上传地址
	Token      string `json:"token"`       // 一次性令牌
}

// UpdateSession 记录一次更新会话的元信息
type UpdateSession struct {
	FileName  string
	FileSize  int64
	ClientIP  string
	ExpiresAt time.Time
}

// TokenStore 用于在内存中维护 token -> UpdateSession 的映射
type TokenStore struct {
	mu       sync.Mutex
	sessions map[string]UpdateSession
}

func NewTokenStore() *TokenStore {
	return &TokenStore{sessions: make(map[string]UpdateSession)}
}

func (s *TokenStore) Put(token string, sess UpdateSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = sess
}

func (s *TokenStore) Get(token string) (UpdateSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return UpdateSession{}, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, token)
		return UpdateSession{}, false
	}
	return sess, true
}

func (s *TokenStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

var globalUpdateTokenStore = NewTokenStore()

// LookupUpdateSession 允许其它组件通过 token 查询更新会话
func LookupUpdateSession(token string) (UpdateSession, bool) {
	return globalUpdateTokenStore.Get(token)
}

// UpdateContext 更新上下文
type UpdateContext struct {
	NodeID string
	Node   *integration.Node
}

// RunLeaderAwareUpdateService 运行 Leader 节点的更新服务
// queueName: 用于客户端发送更新请求的队列名称，例如 "file.update"
// listenIP: 当前节点对外暴露的 TCP 上传 IP
// updatePort: TCP 更新端口（0 表示自动分配）
// tokenTTL: 更新令牌的有效期
func RunLeaderAwareUpdateService(node *integration.Node, queueName, listenIP string, updatePort int, tokenTTL time.Duration) {
	for {
		// 仅在当前节点是 Leader 且 RabbitMQ 连接就绪时提供服务
		if !node.IsLeader() || node.RabbitMQManager == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		mqClient := node.RabbitMQManager.GetClient()
		if mqClient == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		ch := mqClient.Channel()
		if ch == nil {
			log.Printf("[Update][%s] RabbitMQ channel is nil", node.ID)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 声明更新元数据队列
		if _, err := ch.QueueDeclare(
			queueName,
			true,  // durable
			false, // autoDelete
			false, // exclusive
			false, // noWait
			nil,
		); err != nil {
			log.Printf("[Update][%s] declare update queue failed: %v", node.ID, err)
			time.Sleep(time.Second)
			continue
		}

		msgs, err := ch.Consume(
			queueName,
			"update-meta-consumer-"+node.ID,
			false, // autoAck
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			log.Printf("[Update][%s] consume update queue failed: %v", node.ID, err)
			time.Sleep(time.Second)
			continue
		}

		log.Printf("[Update][%s] update metadata service started as leader, queue=%s, listenIP=%s, updatePort=%d",
			node.ID, queueName, listenIP, updatePort)

		// 处理消息
		for d := range msgs {
			handleUpdateMetaMessage(node, ch, &d, listenIP, updatePort, tokenTTL)
		}

		log.Printf("[Update][%s] update metadata service stopped (likely lost leader or connection closed)", node.ID)
		time.Sleep(500 * time.Millisecond)
	}
}

func handleUpdateMetaMessage(node *integration.Node, ch *amqp.Channel, d *amqp.Delivery, listenIP string, updatePort int, tokenTTL time.Duration) {
	defer func() {
		if err := d.Ack(false); err != nil {
			log.Printf("[Update][%s] ack update msg failed: %v", node.ID, err)
		}
	}()

	var args UpdateMetaDataArgs
	if err := json.Unmarshal(d.Body, &args); err != nil {
		log.Printf("[Update][%s] unmarshal update args failed: %v", node.ID, err)
		replyUpdateError(ch, d, "bad request: "+err.Error())
		return
	}

	if args.Operation != "update_file" {
		replyUpdateError(ch, d, "unsupported operation")
		return
	}
	if args.FileName == "" || args.FileSize <= 0 {
		replyUpdateError(ch, d, "invalid file meta")
		return
	}
	if d.ReplyTo == "" {
		log.Printf("[Update][%s] update request without replyTo, file=%s", node.ID, args.FileName)
		return
	}

	log.Printf("[Update][%s] received update request for file: %s", node.ID, args.FileName)

	// 1. 先查询文件是否存在
	fileInfo, err := node.DB.GetFileByName(args.FileName)
	if err != nil {
		// 文件不存在，直接进行新上传
		log.Printf("[Update][%s] file not found, treating as new upload: %s", node.ID, args.FileName)
		handleNewFileUpload(node, ch, d, &args, listenIP, updatePort, tokenTTL)
		return
	}

	log.Printf("[Update][%s] file exists: %s, storage_nodes=%s", node.ID, args.FileName, fileInfo.StorageNodes)

	// 2. 文件存在，准备更新流程
	token := uuid.NewString()
	sess := UpdateSession{
		FileName:  args.FileName,
		FileSize:  args.FileSize,
		ClientIP:  args.ClientIP,
		ExpiresAt: time.Now().Add(tokenTTL),
	}
	globalUpdateTokenStore.Put(token, sess)

	// 如果未显式指定 listenIP，则从节点地址中推导
	if listenIP == "" {
		if host, _, err := net.SplitHostPort(node.Address); err == nil {
			listenIP = host
		} else {
			listenIP = "127.0.0.1"
		}
	}

	ctx := &UpdateContext{
		NodeID: node.ID,
		Node:   node,
	}

	updateAddr, err := StartTCPUpdateServer(listenIP, updatePort, token, tokenTTL, ctx, fileInfo)
	if err != nil {
		log.Printf("[Update][%s] start TCP update server failed: %v", node.ID, err)
		replyUpdateError(ch, d, "start tcp update server failed: "+err.Error())
		return
	}

	// 如果客户端IP是外部地址，将上传地址中的IP替换为localhost
	clientIP := args.ClientIP
	if clientIP != "" && !strings.HasPrefix(clientIP, "172.") && !strings.HasPrefix(clientIP, "192.168.") {
		if host, port, err := net.SplitHostPort(updateAddr); err == nil {
			if strings.HasPrefix(host, "172.") || strings.HasPrefix(host, "192.168.") {
				updateAddr = "localhost:" + port
				log.Printf("[Update][%s] external client detected, converted update address to: %s", node.ID, updateAddr)
			}
		}
	}

	resp := UpdateMetaDataReply{
		OK:         true,
		UpdateAddr: updateAddr,
		Token:      token,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[Update][%s] marshal update reply failed: %v", node.ID, err)
		return
	}

	if err := ch.Publish(
		"",
		d.ReplyTo,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			Body:          body,
			CorrelationId: d.CorrelationId,
		},
	); err != nil {
		log.Printf("[Update][%s] publish update reply failed: %v", node.ID, err)
	}
}

// handleNewFileUpload 文件不存在时作为新上传处理
func handleNewFileUpload(node *integration.Node, ch *amqp.Channel, d *amqp.Delivery, args *UpdateMetaDataArgs, listenIP string, updatePort int, tokenTTL time.Duration) {
	token := uuid.NewString()
	sess := UpdateSession{
		FileName:  args.FileName,
		FileSize:  args.FileSize,
		ClientIP:  args.ClientIP,
		ExpiresAt: time.Now().Add(tokenTTL),
	}
	globalUpdateTokenStore.Put(token, sess)

	if listenIP == "" {
		if host, _, err := net.SplitHostPort(node.Address); err == nil {
			listenIP = host
		} else {
			listenIP = "127.0.0.1"
		}
	}

	ctx := &UpdateContext{
		NodeID: node.ID,
		Node:   node,
	}

	updateAddr, err := StartTCPUpdateServer(listenIP, updatePort, token, tokenTTL, ctx, nil)
	if err != nil {
		log.Printf("[Update][%s] start TCP update server failed: %v", node.ID, err)
		replyUpdateError(ch, d, "start tcp update server failed: "+err.Error())
		return
	}

	// 处理外部客户端地址转换
	clientIP := args.ClientIP
	if clientIP != "" && !strings.HasPrefix(clientIP, "172.") && !strings.HasPrefix(clientIP, "192.168.") {
		if host, port, err := net.SplitHostPort(updateAddr); err == nil {
			if strings.HasPrefix(host, "172.") || strings.HasPrefix(host, "192.168.") {
				updateAddr = "localhost:" + port
				log.Printf("[Update][%s] external client detected, converted update address to: %s", node.ID, updateAddr)
			}
		}
	}

	resp := UpdateMetaDataReply{
		OK:         true,
		UpdateAddr: updateAddr,
		Token:      token,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[Update][%s] marshal update reply failed: %v", node.ID, err)
		return
	}

	if err := ch.Publish(
		"",
		d.ReplyTo,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			Body:          body,
			CorrelationId: d.CorrelationId,
		},
	); err != nil {
		log.Printf("[Update][%s] publish update reply failed: %v", node.ID, err)
	}
}

// StartTCPUpdateServer 启动 TCP 更新服务器
// ttl 指定了监听器的最大生存时间，如果在此时间内无需连接，监听器将关闭
func StartTCPUpdateServer(listenIP string, updatePort int, token string, ttl time.Duration, ctx *UpdateContext, fileInfo *db.FileInfo) (string, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(listenIP, fmt.Sprintf("%d", updatePort)))
	if err != nil {
		return "", fmt.Errorf("failed to start TCP server on %s: %w", listenIP, err)
	}

	updateAddr := listener.Addr().String()
	log.Printf("[Update] TCP update server started for token %s on %s with TTL %v", token, updateAddr, ttl)

	go func() {
		defer listener.Close()

		// 设置 Deadline，防止永久阻塞
		if tcpListener, ok := listener.(*net.TCPListener); ok {
			_ = tcpListener.SetDeadline(time.Now().Add(ttl))
		}

		conn, err := listener.Accept()
		if err != nil {
			// 如果是超时错误，这是预期的行为（清理资源）
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				log.Printf("[Update] listener for token %s timed out (resource cleaned up)", token)
			} else {
				log.Printf("[Update] accept update connection failed: %v", err)
			}
			// 移除过期的 token
			globalUpdateTokenStore.Delete(token)
			return
		}
		defer conn.Close()

		if err := handleSingleUpdate(conn, token, ctx, fileInfo); err != nil {
			log.Printf("[Update] handle update failed: %v", err)
		}
	}()

	return updateAddr, nil
}

// handleSingleUpdate 处理单个更新连接
func handleSingleUpdate(conn net.Conn, expectedToken string, ctx *UpdateContext, fileInfo *db.FileInfo) error {
	reader := bufio.NewReader(conn)

	// 1. 读取首行 token
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read token failed: %w", err)
	}
	receivedToken := strings.TrimSpace(line)
	if receivedToken != expectedToken {
		return fmt.Errorf("token mismatch: expected=%s, got=%s", expectedToken, receivedToken)
	}

	// 2. 查找更新会话
	sess, ok := LookupUpdateSession(expectedToken)
	if !ok {
		return fmt.Errorf("update session not found or expired for token=%s", expectedToken)
	}

	// 3. 将接收的数据保存在本地 FileStorage 目录
	if err := os.MkdirAll("FileStorage", 0o755); err != nil {
		return fmt.Errorf("create FileStorage dir failed: %w", err)
	}
	filePath := filepath.Join("FileStorage", sess.FileName)

	// 如果是更新现有文件，先备份旧文件
	var oldFilePath string
	if fileInfo != nil && fileInfo.LocalPath != "" {
		oldFilePath = fileInfo.LocalPath + ".old"
		if err := os.Rename(fileInfo.LocalPath, oldFilePath); err != nil {
			log.Printf("[Update][%s] backup old file failed: %v", ctx.NodeID, err)
		} else {
			log.Printf("[Update][%s] backed up old file to: %s", ctx.NodeID, oldFilePath)
		}
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	defer file.Close()

	limitReader := io.LimitReader(reader, sess.FileSize)
	written, err := io.Copy(file, limitReader)
	if err != nil {
		return fmt.Errorf("receive file data failed: %w", err)
	}
	if written != sess.FileSize {
		return fmt.Errorf("incomplete update: expect=%d, got=%d", sess.FileSize, written)
	}

	log.Printf("[Update][%s] file update received: path=%s, name=%s, size=%d", ctx.NodeID, filePath, sess.FileName, written)

	// 4. 根据是否存在旧文件信息，决定更新策略
	if fileInfo == nil {
		// 新文件，按照上传流程处理
		return handleNewFileUpdate(ctx, sess, filePath, written)
	} else {
		// 更新现有文件
		return handleExistingFileUpdate(ctx, sess, filePath, written, fileInfo, oldFilePath)
	}
}

// handleNewFileUpdate 处理新文件上传（当文件不存在时）
func handleNewFileUpdate(ctx *UpdateContext, sess UpdateSession, filePath string, fileSize int64) error {
	node := ctx.Node

	// 保存文件信息到数据库
	var fileID int64
	if node.DB != nil {
		fileInfo := db.FileUploadInfo{
			FileName:     sess.FileName,
			FileSize:     fileSize,
			LocalPath:    filePath,
			StorageNodes: ctx.NodeID,
			StorageAdd:   "",
			OwnerID:      sess.ClientIP,
		}

		var err error
		fileID, err = node.DB.SaveFileUpload(fileInfo)
		if err != nil {
			log.Printf("[Update][%s] save file to database failed: %v", ctx.NodeID, err)
		} else {
			log.Printf("[Update][%s] file saved to database: id=%d, name=%s, size=%d", ctx.NodeID, fileID, sess.FileName, fileSize)
		}
	}

	// 复制文件到其他节点
	if node.ReplicationMgr != nil {
		log.Printf("[Update][%s] starting file replication for %s", ctx.NodeID, sess.FileName)
		successNodes, err := node.ReplicationMgr.ReplicateFile(filePath, sess.FileName, fileSize)
		if err != nil {
			log.Printf("[Update][%s] file replication failed: %v", ctx.NodeID, err)
		} else {
			log.Printf("[Update][%s] file replicated to %d nodes: %v", ctx.NodeID, len(successNodes), successNodes)

			// 更新数据库中的 storage_nodes 字段
			if node.DB != nil && fileID > 0 {
				storageNodesStr := strings.Join(successNodes, ",")
				if err := node.DB.UpdateFileStorageNodes(fileID, storageNodesStr); err != nil {
					log.Printf("[Update][%s] update storage nodes failed: %v", ctx.NodeID, err)
				} else {
					log.Printf("[Update][%s] updated storage_nodes in database: %s", ctx.NodeID, storageNodesStr)
				}

				// 向所有其他节点广播元数据
				log.Printf("[Update][%s] broadcasting file metadata to all nodes", ctx.NodeID)
				if err := node.ReplicationMgr.BroadcastMetadata(sess.FileName, fileSize, successNodes, sess.ClientIP); err != nil {
					log.Printf("[Update][%s] metadata broadcast failed: %v", ctx.NodeID, err)
				} else {
					log.Printf("[Update][%s] metadata broadcast completed", ctx.NodeID)
				}
			}
		}
	}

	return nil
}

// handleExistingFileUpdate 处理现有文件的更新
func handleExistingFileUpdate(ctx *UpdateContext, sess UpdateSession, filePath string, fileSize int64,
	fileInfo *db.FileInfo, oldFilePath string) error {
	node := ctx.Node

	// 解析存储节点列表
	storageNodes := strings.Split(fileInfo.StorageNodes, ",")
	log.Printf("[Update][%s] updating file on storage nodes: %v", ctx.NodeID, storageNodes)

	// 检查 Leader 是否本地存储该文件
	leaderStoresFile := false
	for _, nodeID := range storageNodes {
		if strings.TrimSpace(nodeID) == ctx.NodeID {
			leaderStoresFile = true
			break
		}
	}

	log.Printf("[Update][%s] leader stores file: %v", ctx.NodeID, leaderStoresFile)

	// 向所有存储该文件的节点分发更新
	successNodes := []string{}
	for _, nodeID := range storageNodes {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID == "" {
			continue
		}

		if nodeID == ctx.NodeID {
			// Leader 自己已经保存了新文件，标记为成功
			successNodes = append(successNodes, ctx.NodeID)
			log.Printf("[Update][%s] leader node updated locally", ctx.NodeID)
			continue
		}

		// 向其他节点发送更新
		var targetNode *integration.Node
		allNodes := node.Membership.GetAllNodes()
		for _, n := range allNodes {
			if n.ID == nodeID {
				// 找到匹配的节点，需要转换类型
				targetNode = &integration.Node{
					ID:      n.ID,
					Address: n.Address,
				}
				break
			}
		}

		if targetNode == nil {
			log.Printf("[Update][%s] node %s not found in membership", ctx.NodeID, nodeID)
			continue
		}

		log.Printf("[Update][%s] sending update to node %s", ctx.NodeID, nodeID)
		if err := sendUpdateToNode(targetNode, filePath, sess.FileName, fileSize); err != nil {
			log.Printf("[Update][%s] send update to node %s failed: %v", ctx.NodeID, nodeID, err)
		} else {
			successNodes = append(successNodes, nodeID)
			log.Printf("[Update][%s] successfully sent update to node %s", ctx.NodeID, nodeID)
		}
	}

	// 更新数据库元数据（所有节点都需要更新）
	if node.DB != nil {
		// 更新文件大小
		query := `UPDATE files SET file_size = $1, updated_at = NOW() WHERE file_name = $2`
		if _, err := node.DB.Exec(query, fileSize, sess.FileName); err != nil {
			log.Printf("[Update][%s] update file metadata failed: %v", ctx.NodeID, err)
		} else {
			log.Printf("[Update][%s] updated file metadata in database", ctx.NodeID)
		}

		// 如果 Leader 本地存储该文件，更新 local_path
		if leaderStoresFile {
			query = `UPDATE files SET local_path = $1 WHERE file_name = $2`
			if _, err := node.DB.Exec(query, filePath, sess.FileName); err != nil {
				log.Printf("[Update][%s] update local_path failed: %v", ctx.NodeID, err)
			}
		}
	}

	// 向所有节点（包括不存储文件的节点）广播元数据更新
	log.Printf("[Update][%s] broadcasting metadata update to all nodes", ctx.NodeID)
	if err := broadcastMetadataUpdate(node, sess.FileName, fileSize, strings.Join(successNodes, ",")); err != nil {
		log.Printf("[Update][%s] metadata update broadcast failed: %v", ctx.NodeID, err)
	} else {
		log.Printf("[Update][%s] metadata update broadcast completed", ctx.NodeID)
	}

	// 删除备份文件
	if oldFilePath != "" {
		if err := os.Remove(oldFilePath); err != nil {
			log.Printf("[Update][%s] remove old backup file failed: %v", ctx.NodeID, err)
		} else {
			log.Printf("[Update][%s] removed old backup file: %s", ctx.NodeID, oldFilePath)
		}
	}

	log.Printf("[Update][%s] file update completed: %s, updated %d/%d nodes",
		ctx.NodeID, sess.FileName, len(successNodes), len(storageNodes))

	return nil
}

// sendUpdateToNode 向指定节点发送文件更新
func sendUpdateToNode(node *integration.Node, localPath, fileName string, fileSize int64) error {
	// 解析节点地址
	host, _, err := net.SplitHostPort(node.Address)
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到节点的更新服务端口
	updateAddr := net.JoinHostPort(host, "19004") // 更新服务端口

	conn, err := net.DialTimeout("tcp", updateAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(30 * time.Second))

	// 发送更新协议头：UPDATE\n文件名\n文件大小\n
	header := fmt.Sprintf("UPDATE\n%s\n%d\n", fileName, fileSize)
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
	buf := make([]byte, 1<<20) // 1MB
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

// broadcastMetadataUpdate 向所有节点广播元数据更新
func broadcastMetadataUpdate(node *integration.Node, fileName string, fileSize int64, storageNodes string) error {
	allNodes := node.Membership.GetAllNodes()

	successCount := 0
	for _, peerNode := range allNodes {
		if peerNode.ID == node.ID {
			continue // 跳过自己
		}

		log.Printf("[Update][%s] broadcasting metadata update to node %s", node.ID, peerNode.ID)

		if err := sendMetadataUpdateToNode(peerNode, fileName, fileSize, storageNodes); err != nil {
			log.Printf("[Update][%s] broadcast metadata update to node %s failed: %v", node.ID, peerNode.ID, err)
		} else {
			successCount++
			log.Printf("[Update][%s] successfully broadcast metadata update to node %s", node.ID, peerNode.ID)
		}
	}

	log.Printf("[Update][%s] metadata update broadcast: %d/%d nodes", node.ID, successCount, len(allNodes)-1)
	return nil
}

// sendMetadataUpdateToNode 向指定节点发送元数据更新
func sendMetadataUpdateToNode(node interface{}, fileName string, fileSize int64, storageNodes string) error {
	// 获取节点地址
	var nodeAddr string
	switch n := node.(type) {
	case *integration.Node:
		nodeAddr = n.Address
	default:
		// 尝试从 cluster.Node 获取地址
		type ClusterNode interface {
			GetAddress() string
		}
		if cn, ok := node.(ClusterNode); ok {
			nodeAddr = cn.GetAddress()
		} else {
			return fmt.Errorf("unsupported node type")
		}
	}

	host, _, err := net.SplitHostPort(nodeAddr)
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到元数据服务端口
	metadataAddr := net.JoinHostPort(host, "19002")

	conn, err := net.DialTimeout("tcp", metadataAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 发送元数据更新协议：UPDATE_META\n文件名\n文件大小\n存储节点列表\n
	header := fmt.Sprintf("UPDATE_META\n%s\n%d\n%s\n", fileName, fileSize, storageNodes)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send metadata update failed: %w", err)
	}

	// 等待确认
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

func replyUpdateError(ch *amqp.Channel, d *amqp.Delivery, msg string) {
	if d.ReplyTo == "" {
		return
	}
	resp := UpdateMetaDataReply{
		OK:  false,
		Err: msg,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_ = ch.Publish(
		"",
		d.ReplyTo,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			Body:          body,
			CorrelationId: d.CorrelationId,
		},
	)
}
