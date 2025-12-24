package tests

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	DownloadQueueName = "file.download"
	SmallFileSize     = 1024         // 1KB
	MediumFileSize    = 5 * 1 << 20  // 5MB
	LargeFileSize     = 20 * 1 << 20 // 20MB
)

// DownloadMetaDataArgs 下载请求结构
type DownloadMetaDataArgs struct {
	Operation string `json:"operation"`
	ClientIP  string `json:"client_ip"`
	FileName  string `json:"file_name"`
}

// DownloadMetaDataReply 下载响应结构
type DownloadMetaDataReply struct {
	OK           bool   `json:"ok"`
	Err          string `json:"err,omitempty"`
	DownloadAddr string `json:"download_addr"`
	Token        string `json:"token"`
	FileSize     int64  `json:"file_size"`
	FileName     string `json:"file_name"`
}

// TestFileUploadDownloadSmall 测试小文件上传和下载
func TestFileUploadDownloadSmall(t *testing.T) {
	testFileUploadDownload(t, "small_file.bin", SmallFileSize)
}

// TestFileUploadDownloadMedium 测试中等文件上传和下载
func TestFileUploadDownloadMedium(t *testing.T) {
	testFileUploadDownload(t, "medium_file.bin", MediumFileSize)
}

// TestFileUploadDownloadLarge 测试20MB大文件上传和下载
func TestFileUploadDownloadLarge(t *testing.T) {
	testFileUploadDownload(t, "large_file_20mb.bin", LargeFileSize)
}

// testFileUploadDownload 通用的上传下载测试函数
func testFileUploadDownload(t *testing.T, fileName string, fileSize int64) {
	t.Logf("\n=== 文件上传下载测试：%s (大小: %d MB) ===", fileName, fileSize/(1<<20))

	// 步骤1：检查集群状态
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v\n请先运行: .\\scripts\\start_docker_cluster.ps1", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 步骤2：初始化数据库
	t.Log("\n[步骤2] 初始化数据库...")
	if err := initDatabaseSchema(t); err != nil {
		t.Fatalf("数据库初始化失败: %v", err)
	}
	t.Log("✓ 数据库已初始化")

	// 步骤3：生成测试文件
	t.Logf("\n[步骤3] 生成测试文件 (%d 字节)...", fileSize)
	testFile, originalMD5, err := createRandomTestFile(fileName, fileSize)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	defer os.Remove(testFile)
	t.Logf("✓ 测试文件已创建: %s", testFile)
	t.Logf("  原始文件 MD5: %s", originalMD5)

	// 步骤4：上传文件
	t.Log("\n[步骤4] 上传文件...")
	uploadStart := time.Now()
	if err := uploadFileComplete(t, testFile); err != nil {
		t.Fatalf("文件上传失败: %v", err)
	}
	uploadDuration := time.Since(uploadStart)
	uploadSpeed := float64(fileSize) / uploadDuration.Seconds() / (1 << 20) // MB/s
	t.Logf("✓ 文件上传成功")
	t.Logf("  上传耗时: %v", uploadDuration)
	t.Logf("  上传速度: %.2f MB/s", uploadSpeed)

	// 步骤5：等待文件复制
	t.Log("\n[步骤5] 等待文件复制...")
	time.Sleep(3 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤6：验证数据库记录
	t.Log("\n[步骤6] 验证数据库记录...")
	fileInfo, err := verifyDatabaseRecord(t, fileName)
	if err != nil {
		t.Fatalf("数据库验证失败: %v", err)
	}
	t.Logf("✓ 数据库记录正确")
	t.Logf("  文件ID: %d", fileInfo.ID)
	t.Logf("  文件大小: %d 字节", fileInfo.FileSize)
	t.Logf("  存储节点: %s", fileInfo.StorageNodes)

	// 步骤7：下载文件
	t.Log("\n[步骤7] 下载文件...")
	downloadStart := time.Now()
	downloadedFile, err := downloadFileComplete(t, fileName)
	if err != nil {
		t.Fatalf("文件下载失败: %v", err)
	}
	defer os.Remove(downloadedFile)
	downloadDuration := time.Since(downloadStart)
	downloadSpeed := float64(fileSize) / downloadDuration.Seconds() / (1 << 20) // MB/s
	t.Logf("✓ 文件下载成功: %s", downloadedFile)
	t.Logf("  下载耗时: %v", downloadDuration)
	t.Logf("  下载速度: %.2f MB/s", downloadSpeed)

	// 步骤8：验证文件完整性
	t.Log("\n[步骤8] 验证文件完整性...")
	downloadedMD5, err := calculateFileMD5(downloadedFile)
	if err != nil {
		t.Fatalf("计算下载文件MD5失败: %v", err)
	}
	t.Logf("  下载文件 MD5: %s", downloadedMD5)

	if originalMD5 != downloadedMD5 {
		t.Fatalf("MD5 校验失败！原始=%s, 下载=%s", originalMD5, downloadedMD5)
	}
	t.Log("✓ MD5 校验通过，文件完整")

	// 步骤9：验证文件大小
	downloadedInfo, err := os.Stat(downloadedFile)
	if err != nil {
		t.Fatalf("获取下载文件信息失败: %v", err)
	}
	if downloadedInfo.Size() != fileSize {
		t.Fatalf("文件大小不匹配！期望=%d, 实际=%d", fileSize, downloadedInfo.Size())
	}
	t.Log("✓ 文件大小正确")

	// 步骤10：清理测试数据
	t.Log("\n[步骤10] 清理测试数据...")
	if err := cleanupTestFile(t, fileName); err != nil {
		t.Logf("警告: 清理测试文件失败: %v", err)
	}
	t.Log("✓ 测试数据已清理")

	// 测试结果汇总
	t.Log("\n=== 测试结果汇总 ===")
	t.Logf("✓ 文件名: %s", fileName)
	t.Logf("✓ 文件大小: %d 字节 (%.2f MB)", fileSize, float64(fileSize)/(1<<20))
	t.Logf("✓ 上传速度: %.2f MB/s", uploadSpeed)
	t.Logf("✓ 下载速度: %.2f MB/s", downloadSpeed)
	t.Logf("✓ MD5 校验: 通过")
	t.Logf("✓ 存储节点数: %d", len(splitStorageNodes(fileInfo.StorageNodes)))
	t.Log("\n🎉 上传下载测试通过！")
}

// createRandomTestFile 创建随机内容的测试文件
func createRandomTestFile(fileName string, size int64) (string, string, error) {
	tmpFile := filepath.Join(os.TempDir(), fileName)

	file, err := os.Create(tmpFile)
	if err != nil {
		return "", "", fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	// 生成随机数据
	hash := md5.New()
	writer := io.MultiWriter(file, hash)

	// 使用缓冲区来提高性能
	bufSize := 1 << 20 // 1MB buffer
	buffer := make([]byte, bufSize)

	remaining := size
	for remaining > 0 {
		toWrite := int64(bufSize)
		if remaining < toWrite {
			toWrite = remaining
		}

		// 生成随机数据
		if _, err := rand.Read(buffer[:toWrite]); err != nil {
			return "", "", fmt.Errorf("生成随机数据失败: %w", err)
		}

		// 写入文件和计算 MD5
		if _, err := writer.Write(buffer[:toWrite]); err != nil {
			return "", "", fmt.Errorf("写入文件失败: %w", err)
		}

		remaining -= toWrite
	}

	md5sum := hex.EncodeToString(hash.Sum(nil))
	return tmpFile, md5sum, nil
}

// calculateFileMD5 计算文件的 MD5
func calculateFileMD5(filePath string) (string, error) {
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

// uploadFileComplete 完整的上传流程
func uploadFileComplete(t *testing.T, filePath string) error {
	// 1. 请求上传地址
	uploadReply, err := requestUploadAddress(t, filePath)
	if err != nil {
		return fmt.Errorf("请求上传地址失败: %w", err)
	}
	t.Logf("  获得上传地址: %s", uploadReply.UploadAddr)

	// 2. 通过 TCP 上传文件
	if err := uploadFileViaTCP(t, filePath, uploadReply.UploadAddr, uploadReply.Token); err != nil {
		return fmt.Errorf("TCP上传失败: %w", err)
	}

	return nil
}

// downloadFileComplete 完整的下载流程
func downloadFileComplete(t *testing.T, fileName string) (string, error) {
	// 1. 请求下载地址
	downloadReply, err := requestDownloadAddress(t, fileName)
	if err != nil {
		return "", fmt.Errorf("请求下载地址失败: %w", err)
	}
	t.Logf("  获得下载地址: %s", downloadReply.DownloadAddr)
	t.Logf("  Token: %s", downloadReply.Token)
	t.Logf("  文件大小: %d 字节", downloadReply.FileSize)

	// 2. 通过 TCP 下载文件
	downloadedFile := filepath.Join(os.TempDir(), "downloaded_"+fileName)
	if err := downloadFileViaTCP(t, downloadedFile, downloadReply); err != nil {
		return "", fmt.Errorf("TCP下载失败: %w", err)
	}

	return downloadedFile, nil
}

// requestDownloadAddress 通过 RabbitMQ 请求下载地址
func requestDownloadAddress(t *testing.T, fileName string) (*DownloadMetaDataReply, error) {
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

	// 声明下载队列
	_, err = ch.QueueDeclare(DownloadQueueName, true, false, false, false, nil)
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

	// 构造下载请求
	req := DownloadMetaDataArgs{
		Operation: "download",
		ClientIP:  "127.0.0.1",
		FileName:  fileName,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	corrID := uuid.NewString()

	// 发送请求
	err = ch.Publish("", DownloadQueueName, false, false, amqp.Publishing{
		ContentType:   "application/json",
		Body:          body,
		ReplyTo:       replyQ.Name,
		CorrelationId: corrID,
	})
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	// 等待响应
	timeout := time.NewTimer(15 * time.Second)
	defer timeout.Stop()

	for {
		select {
		case d := <-msgs:
			if d.CorrelationId != corrID {
				continue
			}

			var reply DownloadMetaDataReply
			if err := json.Unmarshal(d.Body, &reply); err != nil {
				return nil, fmt.Errorf("解析响应失败: %w", err)
			}

			if !reply.OK {
				return nil, fmt.Errorf("服务器拒绝下载: %s", reply.Err)
			}

			return &reply, nil

		case <-timeout.C:
			return nil, fmt.Errorf("等待响应超时")
		}
	}
}

// downloadFileViaTCP 通过 TCP 下载文件（支持分片接收和进度显示）
func downloadFileViaTCP(t *testing.T, savePath string, reply *DownloadMetaDataReply) error {
	// 如果地址是 Docker 内部 IP，替换为 localhost
	addr := reply.DownloadAddr
	if strings.HasPrefix(addr, "172.") || strings.HasPrefix(addr, "192.168.") {
		if _, port, err := net.SplitHostPort(addr); err == nil {
			addr = "localhost:" + port
			t.Logf("  将 Docker 内部地址转换为: %s", addr)
		}
	}

	// 连接到下载地址
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer conn.Close()

	// 发送 token
	if _, err := conn.Write([]byte(reply.Token + "\n")); err != nil {
		return fmt.Errorf("发送 token 失败: %w", err)
	}

	reader := bufio.NewReader(conn)

	// 读取文件大小
	var fileSize int64
	if _, err := fmt.Fscanf(reader, "%d\n", &fileSize); err != nil {
		return fmt.Errorf("读取文件大小失败: %w", err)
	}

	if fileSize != reply.FileSize {
		t.Logf("  警告: 文件大小不匹配，期望=%d, 实际=%d", reply.FileSize, fileSize)
	}

	// 创建本地文件
	file, err := os.Create(savePath)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer file.Close()

	// 按分片接收文件内容
	const ChunkSize = 1 << 20 // 1MB
	buffer := make([]byte, ChunkSize)
	var received int64
	chunkIndex := 0
	lastProgress := 0

	for received < fileSize {
		n, err := reader.Read(buffer)
		if n > 0 {
			// 写入文件
			if _, writeErr := file.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}

			received += int64(n)
			chunkIndex++

			// 计算并显示进度（每10%显示一次）
			progress := int(float64(received) / float64(fileSize) * 100)
			if progress >= lastProgress+10 || received == fileSize {
				t.Logf("  下载进度: %d%% (%d/%d 字节, 块 %d)",
					progress, received, fileSize, chunkIndex)
				lastProgress = progress
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取数据失败(块 %d): %w", chunkIndex, err)
		}
	}

	if received != fileSize {
		return fmt.Errorf("下载不完整: 期望=%d, 实际=%d", fileSize, received)
	}

	t.Logf("  总共接收 %d 个数据块", chunkIndex)
	return nil
}

// cleanupTestFile 清理测试文件（从数据库中删除）
func cleanupTestFile(t *testing.T, fileName string) error {
	dsns := []string{
		PostgresNode0DSN,
		PostgresNode1DSN,
		PostgresNode2DSN,
		PostgresNode3DSN,
		PostgresNode4DSN,
	}

	for i, dsn := range dsns {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			continue
		}

		_, err = db.Exec("DELETE FROM files WHERE file_name = $1", fileName)
		db.Close()

		if err != nil {
			t.Logf("  节点%d清理失败: %v", i, err)
		}
	}

	return nil
}

// TestFileUploadDownloadConcurrent 并发上传下载测试
func TestFileUploadDownloadConcurrent(t *testing.T) {
	t.Log("\n=== 并发上传下载测试 ===")

	// 检查集群
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v", err)
	}

	// 初始化数据库
	if err := initDatabaseSchema(t); err != nil {
		t.Fatalf("数据库初始化失败: %v", err)
	}

	// 测试3个并发文件上传和下载
	files := []struct {
		name string
		size int64
	}{
		{"concurrent_1.bin", 2 * 1 << 20}, // 2MB
		{"concurrent_2.bin", 3 * 1 << 20}, // 3MB
		{"concurrent_3.bin", 5 * 1 << 20}, // 5MB
	}

	type result struct {
		fileName string
		err      error
		duration time.Duration
	}

	results := make(chan result, len(files))

	// 并发上传
	t.Log("\n[并发上传测试]")
	uploadStart := time.Now()
	for _, f := range files {
		go func(name string, size int64) {
			start := time.Now()
			testFile, _, err := createRandomTestFile(name, size)
			if err == nil {
				defer os.Remove(testFile)
				err = uploadFileComplete(t, testFile)
			}
			results <- result{name, err, time.Since(start)}
		}(f.name, f.size)
	}

	// 收集上传结果
	for i := 0; i < len(files); i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("文件 %s 上传失败: %v", r.fileName, r.err)
		} else {
			t.Logf("✓ %s 上传成功 (耗时: %v)", r.fileName, r.duration)
		}
	}
	uploadTotal := time.Since(uploadStart)
	t.Logf("所有文件上传完成，总耗时: %v", uploadTotal)

	// 等待复制
	time.Sleep(3 * time.Second)

	// 并发下载
	t.Log("\n[并发下载测试]")
	downloadStart := time.Now()
	for _, f := range files {
		go func(name string) {
			start := time.Now()
			downloadedFile, err := downloadFileComplete(t, name)
			if err == nil {
				defer os.Remove(downloadedFile)
			}
			results <- result{name, err, time.Since(start)}
		}(f.name)
	}

	// 收集下载结果
	for i := 0; i < len(files); i++ {
		r := <-results
		if r.err != nil {
			t.Errorf("文件 %s 下载失败: %v", r.fileName, r.err)
		} else {
			t.Logf("✓ %s 下载成功 (耗时: %v)", r.fileName, r.duration)
		}
	}
	downloadTotal := time.Since(downloadStart)
	t.Logf("所有文件下载完成，总耗时: %v", downloadTotal)

	// 清理
	for _, f := range files {
		cleanupTestFile(t, f.name)
	}

	t.Log("\n🎉 并发上传下载测试通过！")
}
