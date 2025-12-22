package delete

import (
	"encoding/json"
	"fmt"
	"log"
	"multi_driver/internal/core/cluster"
	"multi_driver/internal/core/integration"
	"net"
	"time"

	ampq "github.com/rabbitmq/amqp091-go"
)

type DeleteFileArgs struct {
	Operation string `json:"operation"` // "delete"
	FileName  string `json:"file_name"`
}

type DeleteFileReply struct {
	FileName string `json:"file_name"`
	OK       bool   `json:"ok"`
	Err      string `json:"err,omitempty"`
}

func RunLeaderAwareDeleteService(node *integration.Node, queueName string) {
	for {
		if !node.IsLeader() {
			time.Sleep(1 * time.Second)
			continue
		}

		mqClient := node.RabbitMQManager.GetClient()
		if mqClient == nil {
			log.Printf("[Delete][%s] MQ Client is nil", node.ID)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		ch := mqClient.Channel()
		if ch == nil {
			log.Printf("[Delete][%s] Channel is nil", node.ID)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 声明删除队列
		q, err := ch.QueueDeclare(
			queueName,
			true,  // durable
			false, // autoDelete
			false, // exclusive
			false, // noWait
			nil,
		)
		if err != nil {
			log.Printf("[Delete][%s] Failed to declare queue %s: %v", node.ID, queueName, err)
			time.Sleep(2 * time.Second)
			continue
		}
		msgs, err := ch.Consume(
			q.Name,
			"",    // consumer tag
			false, // auto-ack
			false, // exclusive
			false,
			false,
			nil,
		)
		if err != nil {
			log.Printf("[Delete][%s] Failed to consume from queue %s: %v", node.ID, queueName, err)
			time.Sleep(2 * time.Second)
			continue
		}
		log.Printf("[Delete][%s] Listening for delete requests on queue %s", node.ID, queueName)
		for d := range msgs {
			if !node.IsLeader() {
				// 失去 Leader 身份，停止消费
				log.Printf("[Delete][%s] No longer leader, stopping delete service", node.ID)
				_ = d.Nack(false, true) // 重新入队
				break
			}
			handleDeleteMessage(node, ch, d)
		}
		log.Printf("[Delete][%s] Exited delete message loop, restarting...", node.ID)
		time.Sleep(1 * time.Second)
	}

}

func handleDeleteMessage(node *integration.Node, ch *ampq.Channel, d ampq.Delivery) {
	// 解析删除请求
	var deleteArg DeleteFileArgs
	if err := json.Unmarshal(d.Body, &deleteArg); err != nil {
		log.Printf("[Delete][%s] Failed to unmarshal delete request: %v", node.ID, err)
		_ = d.Ack(false)
		replyDeleteError(ch, &d, "bad request: "+err.Error())
		return
	}

	if deleteArg.Operation != "delete" {
		log.Printf("[Delete][%s] Invalid operation: %s", node.ID, deleteArg.Operation)
		_ = d.Ack(false)
		replyDeleteError(ch, &d, "invalid operation")
		return
	}

	if deleteArg.FileName == "" {
		log.Printf("[Delete][%s] Empty file name", node.ID)
		_ = d.Ack(false)
		replyDeleteError(ch, &d, "file name cannot be empty")
		return
	}

	log.Printf("[Delete][%s] Received delete request for file: %s", node.ID, deleteArg.FileName)

	// 1. 先从数据库查询文件信息（获取存储节点列表）
	fileInfo, err := node.DB.GetFileByName(deleteArg.FileName)
	if err != nil {
		log.Printf("[Delete][%s] File not found in database: %s, error: %v", node.ID, deleteArg.FileName, err)
		_ = d.Ack(false)
		replyDeleteError(ch, &d, "file not found")
		return
	}

	log.Printf("[Delete][%s] File %s found, storage nodes: %s", node.ID, deleteArg.FileName, fileInfo.StorageNodes)

	// 2. 使用 ReplicationManager 删除所有节点上的文件
	var successNodes []string
	if node.ReplicationMgr != nil {
		successNodes, err = node.ReplicationMgr.DeleteFile(deleteArg.FileName)
		if err != nil {
			log.Printf("[Delete][%s] Failed to delete file from storage nodes: %v", node.ID, err)
			// 即使部分节点删除失败，也继续删除元数据
		}
		log.Printf("[Delete][%s] Successfully deleted file from nodes: %v", node.ID, successNodes)
	}

	// 3. 从 Redis 缓存中删除
	if node.RedisManager != nil {
		redisClient := node.RedisManager.GetClient()
		if redisClient != nil {
			cacheKey := "file_meta:" + deleteArg.FileName
			if err := redisClient.Delete(cacheKey); err != nil {
				log.Printf("[Delete][%s] Failed to delete from Redis cache: %v", node.ID, err)
			} else {
				log.Printf("[Delete][%s] Deleted file from Redis cache: %s", node.ID, cacheKey)
			}
		}
	}

	// 4. 从本地数据库删除元数据
	if err := node.DB.DeleteFileByName(deleteArg.FileName); err != nil {
		log.Printf("[Delete][%s] Failed to delete from local database: %v", node.ID, err)
		_ = d.Ack(false)
		replyDeleteError(ch, &d, "failed to delete metadata from database")
		return
	}

	log.Printf("[Delete][%s] Successfully deleted file metadata from local database: %s", node.ID, deleteArg.FileName)

	// 5. 向所有其他节点广播删除元数据请求
	allNodes := node.Membership.GetAllNodes()
	for _, peerNode := range allNodes {
		if peerNode.ID == node.ID {
			continue // 跳过自己
		}

		// 向其他节点发送删除元数据的请求
		if err := broadcastDeleteMetadata(peerNode, deleteArg.FileName); err != nil {
			log.Printf("[Delete][%s] Failed to broadcast delete to node %s: %v", node.ID, peerNode.ID, err)
		} else {
			log.Printf("[Delete][%s] Successfully broadcasted delete to node %s", node.ID, peerNode.ID)
		}
	}

	// 6. 确认消息并回复成功
	_ = d.Ack(false)

	reply := DeleteFileReply{
		FileName: deleteArg.FileName,
		OK:       true,
	}
	sendDeleteReply(node, ch, &d, &reply)

	log.Printf("[Delete][%s] Delete operation completed for file: %s", node.ID, deleteArg.FileName)
}

// broadcastDeleteMetadata 向指定节点广播删除元数据请求
func broadcastDeleteMetadata(node *cluster.Node, fileName string) error {
	// 解析节点地址
	host, _, err := net.SplitHostPort(node.Address)
	if err != nil {
		return fmt.Errorf("invalid node address: %w", err)
	}

	// 连接到节点的元数据服务端口
	metadataAddr := net.JoinHostPort(host, "19002")

	conn, err := net.DialTimeout("tcp", metadataAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("dial node failed: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// 发送删除元数据协议：DELETE_META\n文件名\n
	header := fmt.Sprintf("DELETE_META\n%s\n", fileName)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send delete metadata request failed: %w", err)
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

// sendDeleteReply 发送删除成功响应
func sendDeleteReply(node *integration.Node, ch *ampq.Channel, d *ampq.Delivery, reply *DeleteFileReply) {
	if d.ReplyTo == "" {
		return
	}

	body, err := json.Marshal(reply)
	if err != nil {
		log.Printf("[Delete][%s] Failed to marshal reply: %v", node.ID, err)
		return
	}

	err = ch.Publish(
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
	if err != nil {
		log.Printf("[Delete][%s] Failed to publish reply: %v", node.ID, err)
		return
	}

	log.Printf("[Delete][%s] Successfully replied delete for file: %s", node.ID, reply.FileName)
}

// replyDeleteError 发送错误响应
func replyDeleteError(ch *ampq.Channel, d *ampq.Delivery, errMsg string) {
	if d.ReplyTo == "" {
		return
	}

	resp := DeleteFileReply{
		OK:  false,
		Err: errMsg,
	}

	body, err := json.Marshal(resp)
	if err != nil {
		log.Printf("[Delete] Failed to marshal error reply: %v", err)
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
