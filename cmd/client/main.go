// 客户端主程序
// **********************************************
// 对于这个程序，我们认为client只会作为一个向rabbitmq发送请求的角色存在
// 因此不需要实现复杂的raft节点逻辑，也不需要知道leader节点是谁
// **********************************************

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	ampq "github.com/rabbitmq/amqp091-go"
)

const chunkSize = 1 << 20 // 1MB

type Args struct {
	Operation string `json:"operation"`
	// 其他字段根据需要添加
}

type Reply struct {
	OK        bool   `json:"ok"`
	Err       string `json:"err,omitempty"`
	StateCode int    `json:"state_code,omitempty"`
	// 其他字段根据需要添加
}

type Client struct {
	Conn         *ampq.Connection     // RabbitMQ 连接
	Ch           *ampq.Channel        // RabbitMQ 通道
	RequestQueue ampq.Queue           // 用于发送请求的队列
	ReplyQueue   ampq.Queue           // 用于接收回复的临时队列
	Replies      <-chan ampq.Delivery // 用于接收回复消息
}

type UploadMetaDataArgs struct {
	Operation string `json:"operation"` // "upload"
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
}

type UploadMetaDataReply struct {
	OK         bool   `json:"ok"`
	Err        string `json:"err,omitempty"`
	UploadAddr string `json:"upload_addr"` // 例如 "127.0.0.1:29001"
	Token      string `json:"token"`       // 用于上传文件的令牌
}

func NewClient(rabbitmqURL string, requestQueueName string) (*Client, error) {
	// 1. 连接到 RabbitMQ 服务器
	conn, err := ampq.Dial(rabbitmqURL)
	if err != nil {
		return nil, err
	}
	// 2. 创建通道
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	// 声明请求队列（leader 作为 consumer 独占消费）
	q, err := ch.QueueDeclare(
		requestQueueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}

	// 声明一个独占的临时回复队列，用于 RPC
	replyQ, err := ch.QueueDeclare(
		"",    // 让 RabbitMQ 自动生成名字
		false, // durable
		true,  // autoDelete
		true,  // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}

	msgs, err := ch.Consume(
		replyQ.Name,
		"",   // consumer tag
		true, // auto-ack
		true, // exclusive
		false,
		false,
		nil,
	)
	if err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}

	return &Client{
		Conn:         conn,
		Ch:           ch,
		RequestQueue: q,
		ReplyQueue:   replyQ,
		Replies:      msgs,
	}, nil
}

func (c *Client) Close() {
	if c.Ch != nil {
		_ = c.Ch.Close()
	}
	if c.Conn != nil {
		_ = c.Conn.Close()
	}
}

// SendFile：1）通过 RabbitMQ 发“我要上传文件”的请求；2）收到 leader 返回的 TCP 地址后直连传文件
func (c *Client) SendFile(filePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("stat file failed: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", filePath)
	}

	req := UploadMetaDataArgs{
		Operation: "upload_file",
		FileName:  filepath.Base(filePath),
		FileSize:  info.Size(),
	}

	// 通过 RabbitMQ 请求上传
	resp, err := c.rpcUploadRequest(req, 10*time.Second)
	if err != nil {
		return fmt.Errorf("rpc upload request failed: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("leader rejected upload: %s", resp.Err)
	}
	if resp.UploadAddr == "" {
		return fmt.Errorf("empty upload address from leader")
	}

	// 与 leader 提供的 TCP 地址直接建立连接，传输文件内容
	return uploadViaTCP(resp.UploadAddr, resp.Token, filePath)
}

// 通过 RabbitMQ 做一次简单 RPC，请求上传
func (c *Client) rpcUploadRequest(req UploadMetaDataArgs, timeout time.Duration) (*UploadMetaDataReply, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	corrID := uuid.NewString()

	err = c.Ch.Publish(
		"",                  // 默认 exchange，直接路由到队列
		c.RequestQueue.Name, // 请求队列名
		false,
		false,
		ampq.Publishing{
			ContentType:   "application/json",
			Body:          body,
			ReplyTo:       c.ReplyQueue.Name,
			CorrelationId: corrID,
			Type:          "upload_file",
		},
	)
	if err != nil {
		return nil, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case d := <-c.Replies:
			if d.CorrelationId != corrID {
				// 不是这次请求的响应，丢弃或根据需要缓存
				continue
			}
			var resp UploadMetaDataReply
			if err := json.Unmarshal(d.Body, &resp); err != nil {
				return nil, err
			}
			return &resp, nil
		case <-timer.C:
			return nil, fmt.Errorf("wait upload response timeout")
		}
	}
}

type ProgressWriter struct {
	w         io.Writer
	total     int64
	sent      int64
	lastPrint int64
}

func (p *ProgressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.sent += int64(n)
		percent := int64(0)
		if p.total > 0 {
			percent = p.sent * 100 / p.total
		}
		if percent != p.lastPrint {
			fmt.Printf("\r上传进度: %3d%% (%d / %d bytes)", percent, p.sent, p.total)
			p.lastPrint = percent
		}
		if p.sent >= p.total {
			fmt.Println("\n上传完成")
		}
	}
	return n, err
}

// 和 leader 建立 TCP 连接，分片发送 token + 文件内容，并显示进度
func uploadViaTCP(addr, token, filePath string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial leader tcp(%s) failed: %w", addr, err)
	}
	defer conn.Close()

	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("open file failed: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat file failed: %w", err)
	}

	// 简单协议：先发 token + '\n'，再发文件原始字节流
	if _, err := conn.Write([]byte(token + "\n")); err != nil {
		return fmt.Errorf("write token failed: %w", err)
	}

	pw := &ProgressWriter{
		w:     conn,
		total: info.Size(),
	}
	buf := make([]byte, chunkSize)
	if _, err := io.CopyBuffer(pw, f, buf); err != nil {
		return fmt.Errorf("send file data failed: %w", err)
	}

	return nil
}

func main() {
	var (
		amqpURL   = flag.String("amqp", "amqp://guest:guest@localhost:5672/", "RabbitMQ URL")
		queueName = flag.String("queue", "file.upload", "queue name for file upload request")
	)
	flag.Parse()

	args := flag.Args()
	if len(args) < 2 || args[0] != "upload" {
		fmt.Println("用法:")
		fmt.Println("  client upload <filePath> [-amqp amqpURL] [-queue queueName]")
		os.Exit(1)
	}

	filePath := args[1]

	client, err := NewClient(*amqpURL, *queueName)
	if err != nil {
		log.Fatalf("create client failed: %v", err)
	}
	defer client.Close()

	if err := client.SendFile(filePath); err != nil {
		log.Fatalf("send file failed: %v", err)
	}

	fmt.Println("file upload request sent, and file transferred to leader via TCP successfully")
}
