package download

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"multi_driver/internal/core/integration"
)

// DownloadMetaDataArgs 客户端下载元数据请求结构
type DownloadMetaDataArgs struct {
	Operation string `json:"operation"` // "download"
	ClientIP  string `json:"client_ip"`
	FileName  string `json:"file_name"`
}

// DownloadMetaDataReply 返回给客户端的响应，包含下载地址和一次性令牌
type DownloadMetaDataReply struct {
	OK           bool   `json:"ok"`
	Err          string `json:"err,omitempty"`
	DownloadAddr string `json:"download_addr"` // 下载节点的 TCP 地址
	Token        string `json:"token"`         // 下载令牌
	FileSize     int64  `json:"file_size"`     // 文件大小
	FileName     string `json:"file_name"`     // 文件名
}

// DownloadSession 记录一次下载会话的元信息
type DownloadSession struct {
	FileName  string
	FileSize  int64
	FilePath  string // 本地文件路径
	ClientIP  string
	ExpiresAt time.Time
}

// TokenStore 用于在内存中维护 token -> DownloadSession 的映射
type TokenStore struct {
	mu       sync.Mutex
	sessions map[string]DownloadSession
}

func NewTokenStore() *TokenStore {
	return &TokenStore{sessions: make(map[string]DownloadSession)}
}

func (s *TokenStore) Put(token string, sess DownloadSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = sess
}

func (s *TokenStore) Get(token string) (DownloadSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return DownloadSession{}, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, token)
		return DownloadSession{}, false
	}
	return sess, true
}

func (s *TokenStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// globalDownloadTokenStore 提供给同一进程内的 TCP 下载服务使用
var globalDownloadTokenStore = NewTokenStore()

// LookupDownloadSession 允许其它组件通过 token 查询下载会话
func LookupDownloadSession(token string) (DownloadSession, bool) {
	return globalDownloadTokenStore.Get(token)
}

// RunLeaderAwareDownloadService 在节点成为 Leader 时消费下载元数据请求，并返回下载地址和 token
// queueName: 用于客户端发送 DownloadMetaDataArgs 的队列名称，例如 "file.download"
// listenIP: 当前节点对外暴露的 TCP 下载 IP，例如 "10.0.0.5"
// downloadPort: TCP 下载端口（0 表示自动分配）
// tokenTTL: 下载令牌的有效期
func RunLeaderAwareDownloadService(node *integration.Node, queueName, listenIP string, downloadPort int, tokenTTL time.Duration) {
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
			log.Printf("[%s] RabbitMQ channel is nil", node.ID)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 声明下载元数据队列
		if _, err := ch.QueueDeclare(
			queueName,
			true,  // durable
			false, // autoDelete
			false, // exclusive
			false, // noWait
			nil,
		); err != nil {
			log.Printf("[%s] declare download queue failed: %v", node.ID, err)
			time.Sleep(time.Second)
			continue
		}

		msgs, err := ch.Consume(
			queueName,
			"download-meta-consumer-"+node.ID,
			false, // autoAck
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			log.Printf("[%s] consume download queue failed: %v", node.ID, err)
			time.Sleep(time.Second)
			continue
		}

		log.Printf("[%s] download metadata service started as leader, queue=%s, listenIP=%s, downloadPort=%d", node.ID, queueName, listenIP, downloadPort)

		// 当连接关闭或失去 Leader 身份时，RabbitMQManager 会断开连接，msgs 也会被关闭
		for d := range msgs {
			handleDownloadMetaMessage(node, ch, &d, listenIP, downloadPort, tokenTTL)
		}

		log.Printf("[%s] download metadata service stopped (likely lost leader or connection closed)", node.ID)
		// 稍等片刻再重新检查 Leader 状态
		time.Sleep(500 * time.Millisecond)
	}
}

func handleDownloadMetaMessage(node *integration.Node, ch *amqp.Channel, d *amqp.Delivery, listenIP string, downloadPort int, tokenTTL time.Duration) {
	defer func() {
		// 无论成功或失败，都 ack 掉，避免重复消费
		if err := d.Ack(false); err != nil {
			log.Printf("[%s] ack download msg failed: %v", node.ID, err)
		}
	}()

	var args DownloadMetaDataArgs
	if err := json.Unmarshal(d.Body, &args); err != nil {
		log.Printf("[%s] unmarshal download args failed: %v", node.ID, err)
		replyDownloadError(ch, d, "bad request: "+err.Error())
		return
	}

	if args.Operation != "download" {
		replyDownloadError(ch, d, "unsupported operation")
		return
	}
	if args.FileName == "" {
		replyDownloadError(ch, d, "invalid file name")
		return
	}
	if d.ReplyTo == "" {
		log.Printf("[%s] download request without replyTo, file=%s", node.ID, args.FileName)
		return
	}

	// 1. 查询文件元数据，获取存储节点列表
	fileInfo, err := node.DB.GetFileByName(args.FileName)
	if err != nil {
		log.Printf("[%s] file not found: %s, error: %v", node.ID, args.FileName, err)
		replyDownloadError(ch, d, "file not found")
		return
	}

	log.Printf("[%s] file %s found, size=%d, storage_nodes=%s", node.ID, args.FileName, fileInfo.FileSize, fileInfo.StorageNodes)

	// 2. 选择最快的下载节点（包括自己）
	storageNodeIDs := strings.Split(fileInfo.StorageNodes, ",")
	selectedNode, err := selectFastestDownloadNode(node, storageNodeIDs)
	if err != nil {
		log.Printf("[%s] failed to select download node: %v", node.ID, err)
		replyDownloadError(ch, d, "no available download node")
		return
	}

	log.Printf("[%s] selected node %s for download", node.ID, selectedNode.ID)

	// 3. 生成下载令牌
	token := uuid.NewString()
	sess := DownloadSession{
		FileName:  args.FileName,
		FileSize:  fileInfo.FileSize,
		FilePath:  fileInfo.LocalPath,
		ClientIP:  args.ClientIP,
		ExpiresAt: time.Now().Add(tokenTTL),
	}

	// 如果未显式指定 listenIP，则从节点地址中推导 IP
	actualListenIP := listenIP
	if actualListenIP == "" {
		// 使用 0.0.0.0 监听所有接口，这样可以同时接受Docker内部和外部连接
		actualListenIP = "0.0.0.0"
	}

	// 4. 根据选中的节点决定在哪里启动 TCP 服务器
	var downloadAddr string
	if selectedNode.ID == node.ID {
		// 在自己节点上启动 TCP 下载服务器
		globalDownloadTokenStore.Put(token, sess)
		downloadAddr, err = StartTCPDownloadServer("0.0.0.0", downloadPort, token, 5*time.Minute)
		if err != nil {
			log.Printf("[%s] start TCP download server failed: %v", node.ID, err)
			replyDownloadError(ch, d, "start tcp download server failed: "+err.Error())
			return
		}

		// 将 0.0.0.0 替换为节点的实际IP地址
		if strings.HasPrefix(downloadAddr, "0.0.0.0:") || strings.HasPrefix(downloadAddr, "[::]:") {
			if host, _, err := net.SplitHostPort(node.Address); err == nil {
				// 尝试解析主机名为IP
				if ips, err := net.LookupIP(host); err == nil && len(ips) > 0 {
					if _, port, err := net.SplitHostPort(downloadAddr); err == nil {
						downloadAddr = net.JoinHostPort(ips[0].String(), port)
					}
				}
			}
		}
	} else {
		// 在选中的节点上启动 TCP 下载服务器
		downloadAddr, err = requestRemoteDownloadServer(node, selectedNode, token, sess, downloadPort)
		if err != nil {
			log.Printf("[%s] request remote download server failed: %v", node.ID, err)
			replyDownloadError(ch, d, "failed to setup download on remote node: "+err.Error())
			return
		}
	}

	// 5. 处理外部客户端连接（类似上传服务）
	clientIP := args.ClientIP
	if clientIP != "" && !strings.HasPrefix(clientIP, "172.") && !strings.HasPrefix(clientIP, "192.168.") {
		if host, port, err := net.SplitHostPort(downloadAddr); err == nil {
			if strings.HasPrefix(host, "172.") || strings.HasPrefix(host, "192.168.") {
				downloadAddr = "localhost:" + port
				log.Printf("[%s] external client detected, converted download address to: %s", node.ID, downloadAddr)
			}
		}
	}

	// 6. 返回下载地址和令牌
	resp := DownloadMetaDataReply{
		OK:           true,
		DownloadAddr: downloadAddr,
		Token:        token,
		FileSize:     fileInfo.FileSize,
		FileName:     args.FileName,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[%s] marshal download reply failed: %v", node.ID, err)
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
		log.Printf("[%s] publish download reply failed: %v", node.ID, err)
	}

	log.Printf("[%s] download request processed successfully, file=%s, addr=%s", node.ID, args.FileName, downloadAddr)
}

// selectFastestDownloadNode 选择最快的下载节点
// 考虑因素：节点存活性、负载、网络延迟等
// 简化版本：选择第一个可用节点（可以扩展为 ping 测试或负载均衡）
func selectFastestDownloadNode(node *integration.Node, storageNodeIDs []string) (*integration.Node, error) {
	// 获取所有活跃节点
	allNodes := node.Membership.GetAllNodes()

	// 首先检查 leader 自己是否有文件
	for _, nodeID := range storageNodeIDs {
		if nodeID == node.ID {
			log.Printf("[%s] leader has the file, selecting self for download", node.ID)
			return &integration.Node{
				ID:      node.ID,
				Address: node.Address,
			}, nil
		}
	}

	// 在存储节点中查找可用的节点
	for _, nodeID := range storageNodeIDs {
		for _, n := range allNodes {
			if n.ID == nodeID {
				log.Printf("[%s] found available storage node: %s", node.ID, n.ID)
				// TODO: 可以添加 ping 测试或负载检查
				return &integration.Node{
					ID:      n.ID,
					Address: n.Address,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no available storage node found")
}

// requestRemoteDownloadServer 请求远程节点启动下载服务器
func requestRemoteDownloadServer(leaderNode *integration.Node, targetNode *integration.Node, token string, sess DownloadSession, downloadPort int) (string, error) {
	// 解析节点地址
	host, _, err := net.SplitHostPort(targetNode.Address)
	if err != nil {
		return "", fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到节点的控制端口
	controlAddr := net.JoinHostPort(host, "19003") // 下载控制端口

	conn, err := net.DialTimeout("tcp", controlAddr, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 发送启动下载服务器请求
	// 协议：START_DOWNLOAD\ntoken\nfileName\nfileSize\nfilePath\ndownloadPort\n
	request := fmt.Sprintf("START_DOWNLOAD\n%s\n%s\n%d\n%s\n%d\n",
		token, sess.FileName, sess.FileSize, sess.FilePath, downloadPort)

	if _, err := conn.Write([]byte(request)); err != nil {
		return "", fmt.Errorf("send request failed: %w", err)
	}

	// 读取响应：OK\ndownloadAddr\n 或 ERROR\nmessage\n
	response := make([]byte, 1024)
	n, err := conn.Read(response)
	if err != nil {
		return "", fmt.Errorf("read response failed: %w", err)
	}

	responseStr := string(response[:n])
	lines := strings.Split(strings.TrimSpace(responseStr), "\n")
	if len(lines) < 2 {
		return "", fmt.Errorf("invalid response format")
	}

	if lines[0] != "OK" {
		return "", fmt.Errorf("remote node error: %s", lines[1])
	}

	downloadAddr := lines[1]
	log.Printf("[%s] remote download server started at %s", leaderNode.ID, downloadAddr)
	return downloadAddr, nil
}

func replyDownloadError(ch *amqp.Channel, d *amqp.Delivery, msg string) {
	if d.ReplyTo == "" {
		return
	}
	resp := DownloadMetaDataReply{
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
