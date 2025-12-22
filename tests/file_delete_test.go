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

const (
	DeleteQueueName = "file.delete"
)

// 删除请求结构
type DeleteFileArgs struct {
	Operation string `json:"operation"` // "delete"
	FileName  string `json:"file_name"`
}

// 删除响应结构
type DeleteFileReply struct {
	OK       bool   `json:"ok"`
	Err      string `json:"err,omitempty"`
	FileName string `json:"file_name"`
}

// TestFileDeleteIntegration 文件删除集成测试
// 测试完整的删除流程：上传文件 -> 查询验证 -> 删除文件 -> 验证删除成功
func TestFileDeleteIntegration(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 文件删除集成测试 ===")

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
	testFileName := "test_delete_" + uuid.NewString()[:8] + ".txt"
	testFile := createTestFileWithName(t, testFileName)
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

	// 步骤4：等待文件复制和元数据同步
	t.Log("\n[步骤4] 等待文件复制和元数据同步...")
	time.Sleep(8 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤5：查询文件验证上传成功
	t.Log("\n[步骤5] 查询文件验证上传成功...")
	queryReply, err := queryFileMetadata(t, testFileName)
	if err != nil {
		t.Fatalf("查询文件失败: %v", err)
	}

	if !queryReply.OK {
		t.Fatalf("✗ 文件查询失败: %s", queryReply.Err)
	}

	t.Log("✓ 文件查询成功")
	t.Logf("  - 文件名: %s", queryReply.FileName)
	t.Logf("  - 文件大小: %d 字节", queryReply.FileSize)
	t.Logf("  - 存储节点: %s", queryReply.FileStorageAddr)

	// 步骤6：删除文件
	t.Log("\n[步骤6] 删除文件...")
	deleteReply, err := deleteFileMetadata(t, testFileName)
	if err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}

	if !deleteReply.OK {
		t.Fatalf("✗ 删除文件失败: %s", deleteReply.Err)
	}

	t.Log("✓ 删除请求成功")
	t.Logf("  - 已删除文件: %s", deleteReply.FileName)

	// 步骤7：等待删除操作完成和元数据同步
	t.Log("\n[步骤7] 等待删除操作完成...")
	time.Sleep(5 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤8：再次查询文件，验证已被删除
	t.Log("\n[步骤8] 验证文件已被删除...")
	queryReply2, err := queryFileMetadata(t, testFileName)
	if err != nil {
		t.Logf("  查询请求失败（可能是正常的）: %v", err)
	}

	if queryReply2 != nil && queryReply2.OK {
		t.Fatal("✗ 错误: 文件应该已被删除，但仍能查询到")
	}

	t.Log("✓ 验证成功：文件已被删除")
	if queryReply2 != nil {
		t.Logf("  - 错误消息: %s", queryReply2.Err)
	}

	// 步骤9：验证数据库记录已删除
	t.Log("\n[步骤9] 验证数据库记录已删除...")
	fileInfo, err := verifyDatabaseRecord(t, testFileName)
	if err == nil && fileInfo != nil {
		t.Fatal("✗ 错误: 数据库中仍存在文件记录")
	}
	t.Log("✓ 数据库记录已删除")

	// 统计测试结果
	t.Log("\n=== 测试结果汇总 ===")
	t.Log("✓ 步骤1: Docker 集群检查 - 通过")
	t.Log("✓ 步骤2: 数据库初始化 - 通过")
	t.Log("✓ 步骤3: 文件上传 - 通过")
	t.Log("✓ 步骤4: 文件复制同步 - 通过")
	t.Log("✓ 步骤5: 文件查询验证 - 通过")
	t.Log("✓ 步骤6: 文件删除 - 通过")
	t.Log("✓ 步骤7: 删除操作完成 - 通过")
	t.Log("✓ 步骤8: 删除验证 - 通过")
	t.Log("✓ 步骤9: 数据库验证 - 通过")
	t.Log("\n🎉 所有删除测试通过！")
}

// TestFileDeleteNonExistent 测试删除不存在的文件
func TestFileDeleteNonExistent(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 测试删除不存在的文件 ===")

	// 步骤1：检查 Docker 集群状态
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 步骤2：尝试删除不存在的文件
	nonExistentFile := "non_existent_" + uuid.NewString() + ".txt"
	t.Logf("\n[步骤2] 尝试删除不存在的文件: %s", nonExistentFile)

	deleteReply, err := deleteFileMetadata(t, nonExistentFile)
	if err != nil {
		t.Logf("  删除请求失败（预期行为）: %v", err)
	}

	if deleteReply != nil && deleteReply.OK {
		t.Fatal("✗ 错误: 删除不存在的文件应该返回失败")
	}

	t.Log("✓ 正确返回文件不存在")
	if deleteReply != nil {
		t.Logf("  - 错误消息: %s", deleteReply.Err)
	}

	t.Log("\n🎉 测试通过：正确处理不存在的文件")
}

// deleteFileMetadata 通过 RabbitMQ 删除文件
func deleteFileMetadata(t *testing.T, fileName string) (*DeleteFileReply, error) {
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

	// 声明删除队列
	_, err = ch.QueueDeclare(DeleteQueueName, true, false, false, false, nil)
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

	// 构造删除请求
	req := DeleteFileArgs{
		Operation: "delete",
		FileName:  fileName,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	corrID := uuid.NewString()

	// 发送删除请求
	err = ch.Publish("", DeleteQueueName, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQ.Name,
		CorrelationId: corrID,
		Type:          "delete",
	})
	if err != nil {
		return nil, fmt.Errorf("发送删除请求失败: %w", err)
	}

	t.Logf("  已发送删除请求，CorrelationId: %s", corrID)

	// 等待响应
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case d := <-msgs:
			if d.CorrelationId != corrID {
				t.Logf("  收到不匹配的响应，CorrelationId: %s", d.CorrelationId)
				continue
			}

			var reply DeleteFileReply
			if err := json.Unmarshal(d.Body, &reply); err != nil {
				return nil, fmt.Errorf("解析响应失败: %w", err)
			}

			return &reply, nil

		case <-timeout.C:
			return nil, fmt.Errorf("等待删除响应超时")
		}
	}
}

// createTestFileWithName 创建指定名称的测试文件
func createTestFileWithName(t *testing.T, fileName string) string {
	file, err := os.Create(fileName)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(TestFileContent)
	if err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}

	return fileName
}
