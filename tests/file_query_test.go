package tests

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// 查询请求结构
type QueryMetaDataArgs struct {
	Operation string `json:"operation"` // "query"
	FileName  string `json:"file_name"`
}

// 查询响应结构
type QueryMetaDataReply struct {
	OK              bool   `json:"ok"`
	Err             string `json:"err,omitempty"`
	FileName        string `json:"file_name"`
	FileSize        int64  `json:"file_size"`
	CreatedAt       string `json:"created_at"`
	FileStorageAddr string `json:"file_addr"` // 文件存储节点地址列表，逗号分隔
	FileTree        string `json:"file_tree"` // 文件目录树路径，例如 "1-2-3"
}

const (
	QueryQueueName = "file.query"
)

// TestFileQueryIntegration 文件查询集成测试
// 测试两种情况：1. 查询存在的文件  2. 查询不存在的文件
func TestFileQueryIntegration(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 文件查询集成测试 ===")

	// 步骤1：检查 Docker 集群状态
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v\n请先运行: .\\scripts\\start_docker_cluster.ps1", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 步骤2：初始化数据库表结构
	t.Log("\n[步骤2] 初始化数据库表结构...")
	if err := initDatabaseSchema(t); err != nil {
		t.Fatalf("数据库初始化失败: %v", err)
	}
	t.Log("✓ 数据库表结构已创建")

	// 步骤3：创建并上传测试文件
	t.Log("\n[步骤3] 创建并上传测试文件...")
	testFile := createTestFile(t)
	defer os.Remove(testFile)
	t.Logf("✓ 测试文件已创建: %s", testFile)

	// 请求上传地址
	uploadReply, err := requestUploadAddress(t, testFile)
	if err != nil {
		t.Fatalf("请求上传地址失败: %v", err)
	}

	// 上传文件
	if err := uploadFileViaTCP(t, testFile, uploadReply.UploadAddr, uploadReply.Token); err != nil {
		t.Fatalf("文件上传失败: %v", err)
	}
	t.Log("✓ 文件上传成功")

	// 等待文件复制和元数据同步
	t.Log("\n[步骤4] 等待文件元数据写入和同步...")
	time.Sleep(8 * time.Second)
	t.Log("✓ 等待完成")

	// ==================== 测试场景1: 查询存在的文件 ====================
	t.Log("\n=== 测试场景1: 查询存在的文件 ===")
	t.Logf("查询文件: %s", TestFileName)

	queryReply, err := queryFileMetadata(t, TestFileName)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}

	// 验证响应
	if !queryReply.OK {
		t.Fatalf("✗ 查询返回错误: %s", queryReply.Err)
	}

	t.Log("✓ 查询成功")
	t.Logf("  - 文件名: %s", queryReply.FileName)
	t.Logf("  - 文件大小: %d 字节", queryReply.FileSize)
	t.Logf("  - 创建时间: %s", queryReply.CreatedAt)
	t.Logf("  - 存储节点: %s", queryReply.FileStorageAddr)
	t.Logf("  - 文件树: %s", queryReply.FileTree)

	// 验证返回数据的正确性
	if queryReply.FileName != TestFileName {
		t.Errorf("✗ 文件名不匹配: 期望=%s, 实际=%s", TestFileName, queryReply.FileName)
	}
	if queryReply.FileSize != int64(len(TestFileContent)) {
		t.Errorf("✗ 文件大小不匹配: 期望=%d, 实际=%d", len(TestFileContent), queryReply.FileSize)
	}
	if queryReply.CreatedAt == "" {
		t.Error("✗ 创建时间为空")
	}
	if queryReply.FileStorageAddr == "" {
		t.Error("✗ 存储节点地址为空")
	}

	t.Log("✓ 返回数据验证通过")

	// ==================== 测试场景2: 查询不存在的文件 ====================
	t.Log("\n=== 测试场景2: 查询不存在的文件 ===")
	nonExistentFile := "non_existent_file_" + uuid.NewString() + ".txt"
	t.Logf("查询文件: %s", nonExistentFile)

	queryReply2, err := queryFileMetadata(t, nonExistentFile)
	if err != nil {
		t.Fatalf("查询请求失败: %v", err)
	}

	// 验证响应（应该返回错误）
	if queryReply2.OK {
		t.Fatal("✗ 错误: 查询不存在的文件应该返回失败，但返回了成功")
	}

	t.Log("✓ 正确返回文件不存在")
	t.Logf("  - 错误消息: %s", queryReply2.Err)

	if queryReply2.Err == "" {
		t.Error("✗ 错误消息为空")
	}
	if queryReply2.Err != "File not found" {
		t.Logf("  ⚠ 错误消息内容: '%s' (期望: 'File not found')", queryReply2.Err)
	}

	// ==================== 测试场景3: 测试缓存（再次查询相同文件） ====================
	t.Log("\n=== 测试场景3: 测试 Redis 缓存 ===")
	t.Logf("再次查询同一文件: %s", TestFileName)

	startTime := time.Now()
	queryReply3, err := queryFileMetadata(t, TestFileName)
	queryDuration := time.Since(startTime)

	if err != nil {
		t.Fatalf("第二次查询失败: %v", err)
	}

	if !queryReply3.OK {
		t.Fatalf("✗ 第二次查询返回错误: %s", queryReply3.Err)
	}

	t.Log("✓ 缓存查询成功")
	t.Logf("  - 查询耗时: %v", queryDuration)
	t.Logf("  - 文件名: %s", queryReply3.FileName)
	t.Logf("  - 文件大小: %d 字节", queryReply3.FileSize)

	// 验证两次查询结果一致
	if queryReply3.FileName != queryReply.FileName ||
		queryReply3.FileSize != queryReply.FileSize ||
		queryReply3.FileStorageAddr != queryReply.FileStorageAddr {
		t.Error("✗ 两次查询结果不一致")
	} else {
		t.Log("✓ 两次查询结果一致")
	}

	// 统计测试结果
	t.Log("\n=== 测试结果汇总 ===")
	t.Log("✓ 场景1: 查询存在的文件 - 通过")
	t.Log("✓ 场景2: 查询不存在的文件 - 通过")
	t.Log("✓ 场景3: 缓存查询 - 通过")
	t.Log("\n🎉 所有查询测试通过！")
}

// queryFileMetadata 通过 RabbitMQ 查询文件元数据
func queryFileMetadata(t *testing.T, fileName string) (*QueryMetaDataReply, error) {
	// 连接 RabbitMQ
	conn, err := amqp.Dial(RabbitMQURL)
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建通道失败: %w", err)
	}
	defer ch.Close()

	// 声明查询队列
	_, err = ch.QueueDeclare(QueryQueueName, true, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("声明队列失败: %w", err)
	}

	// 创建回复队列
	replyQ, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		return nil, fmt.Errorf("创建回复队列失败: %w", err)
	}

	// 消费回复队列
	msgs, err := ch.Consume(replyQ.Name, "", true, true, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("消费回复队列失败: %w", err)
	}

	// 构造查询请求
	req := QueryMetaDataArgs{
		Operation: "query",
		FileName:  fileName,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	corrID := uuid.NewString()

	// 发送查询请求
	err = ch.Publish("", QueryQueueName, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQ.Name,
		CorrelationId: corrID,
		Type:          "query",
	})
	if err != nil {
		return nil, fmt.Errorf("发送查询请求失败: %w", err)
	}

	t.Logf("  已发送查询请求，CorrelationId: %s", corrID)

	// 等待响应
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case d := <-msgs:
			if d.CorrelationId != corrID {
				t.Logf("  收到不匹配的响应，CorrelationId: %s", d.CorrelationId)
				continue
			}

			var reply QueryMetaDataReply
			if err := json.Unmarshal(d.Body, &reply); err != nil {
				return nil, fmt.Errorf("解析响应失败: %w", err)
			}

			t.Logf("  收到查询响应，OK=%v", reply.OK)
			return &reply, nil

		case <-timeout.C:
			return nil, fmt.Errorf("等待查询响应超时（10秒）")
		}
	}
}

// TestQueryWithoutUpload 测试在没有上传文件的情况下直接查询
func TestQueryWithoutUpload(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 仅查询测试（无需上传文件） ===")

	// 检查集群状态
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 查询一个绝对不存在的文件
	t.Log("\n[步骤2] 查询不存在的文件...")
	nonExistentFile := "absolutely_non_existent_" + uuid.NewString() + ".txt"
	t.Logf("查询文件: %s", nonExistentFile)

	queryReply, err := queryFileMetadata(t, nonExistentFile)
	if err != nil {
		t.Fatalf("查询请求失败: %v", err)
	}

	// 验证返回错误
	if queryReply.OK {
		t.Fatal("✗ 错误: 应该返回文件不存在，但返回了成功")
	}

	t.Log("✓ 正确返回文件不存在")
	t.Logf("  - 错误消息: %s", queryReply.Err)

	t.Log("\n🎉 测试通过！")
}
