package query

import (
	"encoding/json"
	"log"
	"multi_driver/internal/core/integration"
	"time"

	ampq "github.com/rabbitmq/amqp091-go"
)

type QueryMetaDataArgs struct {
	Operation string `json:"operation"` // "query"
	FileName  string `json:"file_name"`
}

type QueryMetaDataReply struct {
	OK              bool   `json:"ok"`
	Err             string `json:"err,omitempty"`
	FileName        string `json:"file_name"`
	FileSize        int64  `json:"file_size"`
	CreatedAt       string `json:"created_at"`
	FileStorageAddr string `json:"file_addr"` // 文件存储节点地址列表，逗号分隔
	FileTree        string `json:"file_tree"` // 文件目录树路径，例如 "1-2-3"
}

// RunLeaderAwareQueryService 启动一个仅在 Leader 节点上运行的查询服务
// 它监听指定的 RabbitMQ 队列，处理查询请求。只有 Leader 节点才处理查询请求
func RunLeaderAwareQueryService(node *integration.Node, queueName string) {
	for {
		if !node.IsLeader() {
			time.Sleep(1 * time.Second)
			continue
		}

		mqClient := node.RabbitMQManager.GetClient()
		if mqClient == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		ch := mqClient.Channel()
		if ch == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		// 声明查询队列
		q, err := ch.QueueDeclare(
			queueName,
			true,  // durable
			false, // autoDelete
			false, // exclusive
			false, // noWait
			nil,
		)
		if err != nil {
			log.Printf("[Query][%s] Failed to declare queue %s: %v", node.ID, queueName, err)
			time.Sleep(2 * time.Second)
			continue
		}

		// 开始消费消息
		msgs, err := ch.Consume(
			q.Name,
			"",    // consumer tag
			false, // auto-ack: false，手动确认
			false, // exclusive
			false, // no-local
			false, // no-wait
			nil,
		)
		if err != nil {
			log.Printf("[Query][%s] Failed to consume from queue %s: %v", node.ID, queueName, err)
			time.Sleep(2 * time.Second)
			continue
		}
		log.Printf("[Query][%s] Started consuming from queue %s", node.ID, queueName)

		// 处理消息
		for d := range msgs {
			if !node.IsLeader() {
				// 失去 Leader 身份，停止消费
				log.Printf("[Query][%s] No longer leader, stopping query service", node.ID)
				_ = d.Nack(false, true) // 重新入队
				break
			}
			handleQueryMessage(node, ch, d)
		}

		// 当连接关闭或失去 Leader 身份时，msgs 会被关闭
		log.Printf("[Query][%s] Query service loop restarting...", node.ID)
		time.Sleep(1 * time.Second)
	}
}

// handleQueryMessage 处理单个查询消息
func handleQueryMessage(node *integration.Node, ch *ampq.Channel, d ampq.Delivery) {
	var args QueryMetaDataArgs
	if err := json.Unmarshal(d.Body, &args); err != nil {
		log.Printf("[Query][%s] Failed to unmarshal query args: %v", node.ID, err)
		_ = d.Ack(false)
		return
	}
	log.Printf("[Query][%s] Received query request for file: %s", node.ID, args.FileName)

	reply := QueryMetaDataReply{
		OK:       false,
		FileName: args.FileName,
	}

	// 查询文件元数据
	// 先在redis中查询是否存在数据
	cacheKey := "file_meta:" + args.FileName

	if node.RedisManager != nil {
		redisClient := node.RedisManager.GetClient()
		if redisClient != nil {
			cachedData, err := redisClient.Get(cacheKey)

			if err == nil && cachedData != "" {
				// Redis 缓存命中
				log.Printf("[Query][%s] Cache hit for file: %s", node.ID, args.FileName)

				if err := json.Unmarshal([]byte(cachedData), &reply); err != nil {
					log.Printf("[Query][%s] Failed to unmarshal cached data: %v", node.ID, err)
					// 缓存数据损坏，继续从数据库查询
				} else {
					reply.OK = true
					_ = d.Ack(false)
					sendQueryReply(node, ch, &d, &reply)
					return
				}
			}
		}
	}

	// Redis 未命中或缓存数据损坏，从 PostgreSQL 查询
	log.Printf("[Query][%s] Cache miss for file: %s, querying database", node.ID, args.FileName)

	row := node.DB.QueryRow(`
		SELECT file_name, file_size, created_at, storage_nodes, storage_add 
		FROM files 
		WHERE file_name = $1
	`, args.FileName)

	var createdAt time.Time
	err := row.Scan(&reply.FileName, &reply.FileSize, &createdAt, &reply.FileStorageAddr, &reply.FileTree)
	if err != nil {
		log.Printf("[Query][%s] File not found: %s, err: %v", node.ID, args.FileName, err)
		_ = d.Ack(false)
		replyQueryError(ch, &d, "File not found")
		return
	}

	// 格式化时间
	reply.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
	reply.OK = true

	log.Printf("[Query][%s] Found file: %s, size: %d", node.ID, reply.FileName, reply.FileSize)

	// 将查询结果放入 Redis，TTL 为 1 小时
	if node.RedisManager != nil {
		redisClient := node.RedisManager.GetClient()
		if redisClient != nil {
			replyJSON, err := json.Marshal(reply)
			if err != nil {
				log.Printf("[Query][%s] Failed to marshal reply for cache: %v", node.ID, err)
			} else { // 存入缓存
				err = redisClient.Set(cacheKey, replyJSON, 1*time.Hour)
				if err != nil {
					log.Printf("[Query][%s] Failed to cache query result: %v", node.ID, err)
				} else {
					log.Printf("[Query][%s] Cached query result for file: %s", node.ID, args.FileName)
				}
			}
		}
	}

	_ = d.Ack(false)
	sendQueryReply(node, ch, &d, &reply)
}

// sendQueryReply 发送查询成功响应
func sendQueryReply(node *integration.Node, ch *ampq.Channel, d *ampq.Delivery, reply *QueryMetaDataReply) {
	body, err := json.Marshal(reply)
	if err != nil {
		log.Printf("[Query][%s] Failed to marshal reply: %v", node.ID, err)
		replyQueryError(ch, d, "Internal server error")
		return
	}

	err = ch.Publish(
		"",        // default exchange
		d.ReplyTo, // routing key: 客户端的回复队列
		false,
		false,
		ampq.Publishing{
			ContentType:   "application/json",
			Body:          body,
			CorrelationId: d.CorrelationId,
		},
	)
	if err != nil {
		log.Printf("[Query][%s] Failed to publish reply: %v", node.ID, err)
		return
	}

	log.Printf("[Query][%s] Successfully replied query for file: %s", node.ID, reply.FileName)
}

// replyQueryError 发送错误响应给客户端
func replyQueryError(ch *ampq.Channel, d *ampq.Delivery, errMsg string) {
	if d.ReplyTo == "" {
		return
	}

	resp := QueryMetaDataReply{
		OK:  false,
		Err: errMsg,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[Query] Failed to marshal error reply: %v", err)
		return
	}

	_ = ch.Publish(
		"",
		d.ReplyTo,
		false,
		false,
		ampq.Publishing{
			ContentType:   "application/json",
			Body:          body,
			CorrelationId: d.CorrelationId,
		},
	)
}
