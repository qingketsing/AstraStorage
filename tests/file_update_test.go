package tests

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	UpdateQueueName = "file.update"
)

// UpdateMetaDataArgs 更新请求结构
type UpdateMetaDataArgs struct {
	Operation string `json:"operation"` // "update_file"
	ClientIP  string `json:"client_ip"`
	FileName  string `json:"file_name"`
	FileSize  int64  `json:"file_size"`
}

// UpdateMetaDataReply 更新响应结构
type UpdateMetaDataReply struct {
	OK         bool   `json:"ok"`
	Err        string `json:"err,omitempty"`
	UpdateAddr string `json:"update_addr"` // 更新上传地址
	Token      string `json:"token"`       // 一次性令牌
}

// TestFileUpdateExisting 测试更新现有文件
func TestFileUpdateExisting(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 文件更新集成测试（更新现有文件）===")

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

	// 步骤3：上传原始文件
	t.Log("\n[步骤3] 上传原始文件...")
	testFileName := "test_update_" + uuid.NewString()[:8] + ".txt"
	originalContent := "Original content - Version 1.0\nThis is the first version.\n"
	originalFile := createTestFileWithContent(t, testFileName, originalContent)
	defer os.Remove(originalFile)

	// 请求上传地址
	uploadReply, err := requestUploadAddress(t, originalFile)
	if err != nil {
		t.Fatalf("请求上传地址失败: %v", err)
	}

	// 上传原始文件
	if err := uploadFileViaTCP(t, originalFile, uploadReply.UploadAddr, uploadReply.Token); err != nil {
		t.Fatalf("原始文件上传失败: %v", err)
	}
	originalMD5, _ := calculateFileMD5(originalFile)
	t.Logf("✓ 原始文件上传成功 (MD5: %s)", originalMD5)

	// 步骤4：等待文件复制
	t.Log("\n[步骤4] 等待文件复制...")
	time.Sleep(6 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤5：验证原始文件
	t.Log("\n[步骤5] 验证原始文件...")
	fileInfo, err := queryAndVerifyFile(t, testFileName, int64(len(originalContent)))
	if err != nil {
		t.Fatalf("验证原始文件失败: %v", err)
	}
	t.Logf("✓ 原始文件验证成功")
	t.Logf("  - 文件大小: %d 字节", fileInfo.FileSize)
	t.Logf("  - 存储节点: %s", fileInfo.StorageNodes)

	// 步骤6：创建更新后的文件
	t.Log("\n[步骤6] 创建更新后的文件...")
	updatedContent := "Updated content - Version 2.0\nThis is the updated version with more data.\nLine 3\nLine 4\n"
	updatedFile := createTestFileWithContent(t, testFileName+"_updated", updatedContent)
	defer os.Remove(updatedFile)
	updatedMD5, _ := calculateFileMD5(updatedFile)
	t.Logf("✓ 更新文件已创建 (MD5: %s, 大小: %d 字节)", updatedMD5, len(updatedContent))

	// 步骤7：请求更新地址
	t.Log("\n[步骤7] 请求更新地址...")
	updateReply, err := requestUpdateAddress(t, testFileName, int64(len(updatedContent)))
	if err != nil {
		t.Fatalf("请求更新地址失败: %v", err)
	}
	t.Logf("✓ 获得更新地址: %s, Token: %s", updateReply.UpdateAddr, updateReply.Token)

	// 步骤8：上传更新文件
	t.Log("\n[步骤8] 上传更新文件...")
	if err := uploadFileViaTCP(t, updatedFile, updateReply.UpdateAddr, updateReply.Token); err != nil {
		t.Fatalf("更新文件上传失败: %v", err)
	}
	t.Log("✓ 更新文件上传成功")

	// 步骤9：等待更新完成
	t.Log("\n[步骤9] 等待更新完成...")
	time.Sleep(8 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤10：验证更新后的文件元数据
	t.Log("\n[步骤10] 验证更新后的文件元数据...")
	updatedFileInfo, err := queryAndVerifyFile(t, testFileName, int64(len(updatedContent)))
	if err != nil {
		t.Fatalf("验证更新后文件失败: %v", err)
	}
	t.Logf("✓ 文件元数据已更新")
	t.Logf("  - 原始大小: %d 字节", fileInfo.FileSize)
	t.Logf("  - 新大小: %d 字节", updatedFileInfo.FileSize)
	t.Logf("  - 存储节点: %s", updatedFileInfo.StorageNodes)

	// 步骤11：下载并验证更新后的文件内容
	t.Log("\n[步骤11] 下载并验证更新后的文件内容...")
	downloadedFile, err := downloadFileComplete(t, testFileName)
	if err != nil {
		t.Fatalf("下载更新后文件失败: %v", err)
	}
	defer os.Remove(downloadedFile)

	downloadedMD5, _ := calculateFileMD5(downloadedFile)
	t.Logf("  - 期望 MD5: %s", updatedMD5)
	t.Logf("  - 下载 MD5: %s", downloadedMD5)

	if downloadedMD5 != updatedMD5 {
		t.Fatalf("✗ MD5 校验失败！期望=%s, 实际=%s", updatedMD5, downloadedMD5)
	}
	t.Log("✓ 文件内容验证成功，MD5 匹配")

	// 步骤12：清理测试数据
	t.Log("\n[步骤12] 清理测试数据...")
	if err := cleanupTestFile(t, testFileName); err != nil {
		t.Logf("警告: 清理测试文件失败: %v", err)
	}
	t.Log("✓ 测试数据已清理")

	// 测试结果汇总
	t.Log("\n=== 测试结果汇总 ===")
	t.Log("✓ 原始文件上传成功")
	t.Log("✓ 原始文件验证通过")
	t.Log("✓ 文件更新上传成功")
	t.Log("✓ 更新后元数据正确")
	t.Log("✓ 更新后内容验证通过")
	t.Logf("✓ 存储节点数: %d", len(splitStorageNodes(updatedFileInfo.StorageNodes)))
	t.Log("\n🎉 文件更新测试通过！")
}

// TestFileUpdateNonExistent 测试更新不存在的文件（应作为新上传处理）
func TestFileUpdateNonExistent(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 文件更新集成测试（文件不存在）===")

	// 步骤1：检查集群状态
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 步骤2：初始化数据库
	t.Log("\n[步骤2] 初始化数据库...")
	if err := initDatabaseSchema(t); err != nil {
		t.Fatalf("数据库初始化失败: %v", err)
	}
	t.Log("✓ 数据库已初始化")

	// 步骤3：创建测试文件
	t.Log("\n[步骤3] 创建测试文件（文件不存在于系统中）...")
	testFileName := "test_update_new_" + uuid.NewString()[:8] + ".txt"
	content := "New file via update operation\nThis file does not exist before.\n"
	testFile := createTestFileWithContent(t, testFileName, content)
	defer os.Remove(testFile)
	fileMD5, _ := calculateFileMD5(testFile)
	t.Logf("✓ 测试文件已创建 (MD5: %s)", fileMD5)

	// 步骤4：请求更新地址（文件不存在）
	t.Log("\n[步骤4] 请求更新地址...")
	updateReply, err := requestUpdateAddress(t, testFileName, int64(len(content)))
	if err != nil {
		t.Fatalf("请求更新地址失败: %v", err)
	}
	t.Logf("✓ 获得更新地址: %s, Token: %s", updateReply.UpdateAddr, updateReply.Token)

	// 步骤5：上传文件
	t.Log("\n[步骤5] 上传文件...")
	if err := uploadFileViaTCP(t, testFile, updateReply.UpdateAddr, updateReply.Token); err != nil {
		t.Fatalf("文件上传失败: %v", err)
	}
	t.Log("✓ 文件上传成功")

	// 步骤6：等待复制
	t.Log("\n[步骤6] 等待文件复制...")
	time.Sleep(6 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤7：验证文件
	t.Log("\n[步骤7] 验证文件...")
	fileInfo, err := queryAndVerifyFile(t, testFileName, int64(len(content)))
	if err != nil {
		t.Fatalf("验证文件失败: %v", err)
	}
	t.Logf("✓ 文件验证成功")
	t.Logf("  - 文件大小: %d 字节", fileInfo.FileSize)
	t.Logf("  - 存储节点: %s", fileInfo.StorageNodes)

	// 步骤8：下载并验证内容
	t.Log("\n[步骤8] 下载并验证内容...")
	downloadedFile, err := downloadFileComplete(t, testFileName)
	if err != nil {
		t.Fatalf("下载文件失败: %v", err)
	}
	defer os.Remove(downloadedFile)

	downloadedMD5, _ := calculateFileMD5(downloadedFile)
	if downloadedMD5 != fileMD5 {
		t.Fatalf("✗ MD5 校验失败！期望=%s, 实际=%s", fileMD5, downloadedMD5)
	}
	t.Log("✓ 文件内容验证成功")

	// 步骤9：清理
	t.Log("\n[步骤9] 清理测试数据...")
	if err := cleanupTestFile(t, testFileName); err != nil {
		t.Logf("警告: 清理测试文件失败: %v", err)
	}
	t.Log("✓ 测试数据已清理")

	// 测试结果汇总
	t.Log("\n=== 测试结果汇总 ===")
	t.Log("✓ 通过更新接口创建新文件成功")
	t.Log("✓ 文件元数据正确")
	t.Log("✓ 文件内容验证通过")
	t.Log("\n🎉 文件更新（新文件）测试通过！")
}

// TestFileUpdateMultipleTimes 测试多次更新同一文件
func TestFileUpdateMultipleTimes(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 文件多次更新测试 ===")

	// 步骤1：检查集群
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 步骤2：初始化数据库
	t.Log("\n[步骤2] 初始化数据库...")
	if err := initDatabaseSchema(t); err != nil {
		t.Fatalf("数据库初始化失败: %v", err)
	}
	t.Log("✓ 数据库已初始化")

	// 步骤3：上传初始文件
	t.Log("\n[步骤3] 上传初始文件（版本1）...")
	testFileName := "test_update_multi_" + uuid.NewString()[:8] + ".txt"
	version1Content := "Version 1\n"
	version1File := createTestFileWithContent(t, testFileName+"_v1", version1Content)
	defer os.Remove(version1File)

	uploadReply, err := requestUploadAddress(t, version1File)
	if err != nil {
		t.Fatalf("请求上传地址失败: %v", err)
	}
	if err := uploadFileViaTCP(t, version1File, uploadReply.UploadAddr, uploadReply.Token); err != nil {
		t.Fatalf("版本1上传失败: %v", err)
	}
	t.Log("✓ 版本1上传成功")
	time.Sleep(5 * time.Second)

	// 步骤4：更新到版本2
	t.Log("\n[步骤4] 更新到版本2...")
	version2Content := "Version 2 - Updated content\n"
	version2File := createTestFileWithContent(t, testFileName+"_v2", version2Content)
	defer os.Remove(version2File)

	updateReply2, err := requestUpdateAddress(t, testFileName, int64(len(version2Content)))
	if err != nil {
		t.Fatalf("请求更新地址失败: %v", err)
	}
	if err := uploadFileViaTCP(t, version2File, updateReply2.UpdateAddr, updateReply2.Token); err != nil {
		t.Fatalf("版本2上传失败: %v", err)
	}
	t.Log("✓ 版本2更新成功")
	time.Sleep(5 * time.Second)

	// 步骤5：更新到版本3
	t.Log("\n[步骤5] 更新到版本3...")
	version3Content := "Version 3 - Even more content\nWith multiple lines\n"
	version3File := createTestFileWithContent(t, testFileName+"_v3", version3Content)
	defer os.Remove(version3File)

	updateReply3, err := requestUpdateAddress(t, testFileName, int64(len(version3Content)))
	if err != nil {
		t.Fatalf("请求更新地址失败: %v", err)
	}
	if err := uploadFileViaTCP(t, version3File, updateReply3.UpdateAddr, updateReply3.Token); err != nil {
		t.Fatalf("版本3上传失败: %v", err)
	}
	t.Log("✓ 版本3更新成功")
	time.Sleep(5 * time.Second)

	// 步骤6：验证最终版本
	t.Log("\n[步骤6] 验证最终版本...")
	fileInfo, err := queryAndVerifyFile(t, testFileName, int64(len(version3Content)))
	if err != nil {
		t.Fatalf("验证文件失败: %v", err)
	}
	t.Logf("✓ 文件元数据正确（大小: %d 字节）", fileInfo.FileSize)

	// 步骤7：下载并验证
	t.Log("\n[步骤7] 下载并验证最终内容...")
	downloadedFile, err := downloadFileComplete(t, testFileName)
	if err != nil {
		t.Fatalf("下载文件失败: %v", err)
	}
	defer os.Remove(downloadedFile)

	version3MD5, _ := calculateFileMD5(version3File)
	downloadedMD5, _ := calculateFileMD5(downloadedFile)
	if downloadedMD5 != version3MD5 {
		t.Fatalf("✗ MD5 校验失败！期望=%s, 实际=%s", version3MD5, downloadedMD5)
	}
	t.Log("✓ 最终版本内容正确")

	// 步骤8：清理
	t.Log("\n[步骤8] 清理测试数据...")
	if err := cleanupTestFile(t, testFileName); err != nil {
		t.Logf("警告: 清理测试文件失败: %v", err)
	}
	t.Log("✓ 测试数据已清理")

	// 测试结果汇总
	t.Log("\n=== 测试结果汇总 ===")
	t.Log("✓ 版本1上传成功")
	t.Log("✓ 版本2更新成功")
	t.Log("✓ 版本3更新成功")
	t.Log("✓ 最终版本验证通过")
	t.Log("\n🎉 多次更新测试通过！")
}

// requestUpdateAddress 请求文件更新地址
func requestUpdateAddress(t *testing.T, fileName string, fileSize int64) (*UpdateMetaDataReply, error) {
	conn, err := amqp.Dial(RabbitMQURL)
	if err != nil {
		return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("创建 channel 失败: %w", err)
	}
	defer ch.Close()

	// 声明回复队列
	replyQueue, err := ch.QueueDeclare(
		"",    // name
		false, // durable
		true,  // autoDelete
		true,  // exclusive
		false, // noWait
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("声明回复队列失败: %w", err)
	}

	// 开始消费回复
	msgs, err := ch.Consume(
		replyQueue.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("消费回复队列失败: %w", err)
	}

	// 准备更新请求
	correlationID := uuid.NewString()
	updateArgs := UpdateMetaDataArgs{
		Operation: "update_file",
		ClientIP:  "127.0.0.1",
		FileName:  fileName,
		FileSize:  fileSize,
	}

	body, err := json.Marshal(updateArgs)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 发送更新请求
	err = ch.Publish(
		"",
		UpdateQueueName,
		false,
		false,
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: correlationID,
			ReplyTo:       replyQueue.Name,
			Body:          body,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("发送更新请求失败: %w", err)
	}

	// 等待响应
	select {
	case msg := <-msgs:
		if msg.CorrelationId != correlationID {
			return nil, fmt.Errorf("correlation ID 不匹配")
		}

		var reply UpdateMetaDataReply
		if err := json.Unmarshal(msg.Body, &reply); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}

		if !reply.OK {
			return nil, fmt.Errorf("更新请求失败: %s", reply.Err)
		}

		return &reply, nil

	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("等待更新响应超时")
	}
}

// createTestFileWithContent 创建包含指定内容的测试文件
func createTestFileWithContent(t *testing.T, fileName, content string) string {
	tmpFile := filepath.Join(os.TempDir(), fileName)
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	return tmpFile
}

// queryAndVerifyFile 查询并验证文件元数据（带自定义文件大小验证）
func queryAndVerifyFile(t *testing.T, fileName string, expectedSize int64) (*FileInfo, error) {
	dsns := []string{
		PostgresNode0DSN,
		PostgresNode1DSN,
		PostgresNode2DSN,
		PostgresNode3DSN,
		PostgresNode4DSN,
	}

	var fileInfo FileInfo
	var found bool

	for i := 0; i < 10; i++ {
		for nodeIdx, dsn := range dsns {
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				continue
			}

			var localPath sql.NullString
			err = db.QueryRow(`
				SELECT id, file_name, file_size, local_path, 
				       COALESCE(storage_nodes, ''), COALESCE(storage_add, ''), 
				       COALESCE(owner_id, ''), created_at
				FROM files 
				WHERE file_name = $1
				ORDER BY created_at DESC
				LIMIT 1
			`, fileName).Scan(
				&fileInfo.ID,
				&fileInfo.FileName,
				&fileInfo.FileSize,
				&localPath,
				&fileInfo.StorageNodes,
				&fileInfo.StorageAdd,
				&fileInfo.OwnerID,
				&fileInfo.CreatedAt,
			)

			if localPath.Valid {
				fileInfo.LocalPath = localPath.String
			}

			db.Close()

			if err == nil {
				found = true
				t.Logf("  在节点%d找到文件记录", nodeIdx)
				break
			}
		}

		if found {
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	if !found {
		return nil, fmt.Errorf("未找到文件记录: %s", fileName)
	}

	// 验证文件大小
	if fileInfo.FileSize != expectedSize {
		return nil, fmt.Errorf("文件大小不匹配: 期望=%d, 实际=%d",
			expectedSize, fileInfo.FileSize)
	}

	// 验证存储节点
	if fileInfo.StorageNodes == "" {
		return nil, fmt.Errorf("storage_nodes 为空")
	}

	return &fileInfo, nil
}
