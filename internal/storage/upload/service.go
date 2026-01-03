// 这部分是处理上传服务的代码
package upload

import (
	"encoding/json"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"multi_driver/internal/core/integration"
)

// UploadMetaDataArgs 与客户端约定的上传元数据请求结构保持一致
type UploadMetaDataArgs struct {
	Operation string `json:"operation"`
	ClientIP  string `json:"client_ip"`
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
}

// UploadMetaDataReply 返回给客户端的响应，包含上传地址和一次性令牌
type UploadMetaDataReply struct {
	OK         bool   `json:"ok"`
	Err        string `json:"err,omitempty"`
	UploadAddr string `json:"upload_addr"`
	Token      string `json:"token"`
}

// UploadSession 记录一次上传会话的元信息
type UploadSession struct {
	FileName  string
	FileSize  int64
	ClientIP  string
	ExpiresAt time.Time
}

// TokenStore 用于在内存中维护 token -> UploadSession 的映射
type TokenStore struct {
	mu       sync.Mutex
	sessions map[string]UploadSession
}

func NewTokenStore() *TokenStore {
	return &TokenStore{sessions: make(map[string]UploadSession)}
}

func (s *TokenStore) Put(token string, sess UploadSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = sess
}

func (s *TokenStore) Get(token string) (UploadSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return UploadSession{}, false
	}
	if time.Now().After(sess.ExpiresAt) {
		delete(s.sessions, token)
		return UploadSession{}, false
	}
	return sess, true
}

func (s *TokenStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// globalTokenStore 提供给同一进程内的 TCP 上传服务使用（例如通过 token 查会话）
var globalTokenStore = NewTokenStore()

// LookupUploadSession 允许其它组件通过 token 查询上传会话
func LookupUploadSession(token string) (UploadSession, bool) {
	return globalTokenStore.Get(token)
}

// RunLeaderAwareUploadService 在节点成为 Leader 时消费上传元数据请求，并返回上传地址和 token
// queueName: 用于客户端发送 UploadMetaDataArgs 的队列名称，例如 "file.upload"
// listenIP: 当前节点对外暴露的 TCP 上传 IP，例如 "10.0.0.5"
// uploadPort: TCP 上传端口（0 表示自动分配）
// tokenTTL: 上传令牌的有效期
func RunLeaderAwareUploadService(node *integration.Node, queueName, listenIP string, uploadPort int, tokenTTL time.Duration) {
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

		// 声明上传元数据队列
		if _, err := ch.QueueDeclare(
			queueName,
			true,  // durable
			false, // autoDelete
			false, // exclusive
			false, // noWait
			nil,
		); err != nil {
			log.Printf("[%s] declare upload queue failed: %v", node.ID, err)
			time.Sleep(time.Second)
			continue
		}

		msgs, err := ch.Consume(
			queueName,
			"upload-meta-consumer-"+node.ID,
			false, // autoAck
			false,
			false,
			false,
			nil,
		)
		if err != nil {
			log.Printf("[%s] consume upload queue failed: %v", node.ID, err)
			time.Sleep(time.Second)
			continue
		}

		log.Printf("[%s] upload metadata service started as leader, queue=%s, listenIP=%s, uploadPort=%d", node.ID, queueName, listenIP, uploadPort)

		// 当连接关闭或失去 Leader 身份时，RabbitMQManager 会断开连接，msgs 也会被关闭
		for d := range msgs {
			handleUploadMetaMessage(node, ch, &d, listenIP, uploadPort, tokenTTL)
		}

		log.Printf("[%s] upload metadata service stopped (likely lost leader or connection closed)", node.ID)
		// 稍等片刻再重新检查 Leader 状态
		time.Sleep(500 * time.Millisecond)
	}
}

func handleUploadMetaMessage(node *integration.Node, ch *amqp.Channel, d *amqp.Delivery, listenIP string, uploadPort int, tokenTTL time.Duration) {
	defer func() {
		// 无论成功或失败，都 ack 掉，避免重复消费；如果需要重试可根据业务调整
		if err := d.Ack(false); err != nil {
			log.Printf("[%s] ack upload msg failed: %v", node.ID, err)
		}
	}()

	var args UploadMetaDataArgs
	if err := json.Unmarshal(d.Body, &args); err != nil {
		log.Printf("[%s] unmarshal upload args failed: %v", node.ID, err)
		replyUploadError(ch, d, "bad request: "+err.Error())
		return
	}

	if args.Operation != "upload_file" {
		replyUploadError(ch, d, "unsupported operation")
		return
	}
	if args.FileName == "" || args.FileSize <= 0 {
		replyUploadError(ch, d, "invalid file meta")
		return
	}
	if d.ReplyTo == "" {
		// 没有回复队列，记录日志后略过
		log.Printf("[%s] upload request without replyTo, file=%s", node.ID, args.FileName)
		return
	}

	token := uuid.NewString()
	sess := UploadSession{
		FileName:  args.FileName,
		FileSize:  args.FileSize,
		ExpiresAt: time.Now().Add(tokenTTL),
	}
	globalTokenStore.Put(token, sess)

	// 如果未显式指定 listenIP，则从节点地址中推导 IP
	if listenIP == "" {
		if host, _, err := net.SplitHostPort(node.Address); err == nil {
			listenIP = host
		} else {
			listenIP = "127.0.0.1"
		}
	}

	ctx := &UploadContext{
		NodeID: node.ID,
		DB:     node.DB,
	}
	// 如果节点有ReplicationManager，传入以便执行复制
	if node.ReplicationMgr != nil {
		ctx.ReplicationMgr = node.ReplicationMgr
	}

	uploadAddr, err := StartTCPUploadServer(listenIP, uploadPort, token, tokenTTL, ctx)
	if err != nil {
		log.Printf("[%s] start TCP upload server failed: %v", node.ID, err)
		replyUploadError(ch, d, "start tcp upload server failed: "+err.Error())
		return
	}

	// 如果客户端IP是外部地址（非Docker内部），将上传地址中的IP替换为localhost
	// 这样测试可以从宿主机连接到容器
	clientIP := args.ClientIP
	// Docker内部IP通常是172.x.x.x，客户端如果不是这个范围，说明是外部连接
	if clientIP != "" && !strings.HasPrefix(clientIP, "172.") && !strings.HasPrefix(clientIP, "192.168.") {
		// 客户端是外部地址，将 Docker 内部 IP 替换为 localhost
		if host, port, err := net.SplitHostPort(uploadAddr); err == nil {
			if strings.HasPrefix(host, "172.") || strings.HasPrefix(host, "192.168.") {
				uploadAddr = "localhost:" + port
				log.Printf("[%s] external client detected, converted upload address to: %s", node.ID, uploadAddr)
			}
		}
	}

	resp := UploadMetaDataReply{
		OK:         true,
		UploadAddr: uploadAddr,
		Token:      token,
	}
	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[%s] marshal upload reply failed: %v", node.ID, err)
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
		log.Printf("[%s] publish upload reply failed: %v", node.ID, err)
	}
}

func replyUploadError(ch *amqp.Channel, d *amqp.Delivery, msg string) {
	if d.ReplyTo == "" {
		return
	}
	resp := UploadMetaDataReply{
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
