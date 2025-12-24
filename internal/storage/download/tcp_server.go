package download

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"
)

const (
	// ChunkSize 分片大小：1MB
	ChunkSize = 1 << 20 // 1MB
)

// StartTCPDownloadServer 为一次下载请求启动一个 TCP 监听
// listenIP 是当前节点对外可达的 IP
// downloadPort 是指定的下载端口（0 表示系统自动分配）
// token 用于在第一步校验连接是否合法
// 返回值 downloadAddr 是客户端应当使用的 "ip:port" 地址
func StartTCPDownloadServer(listenIP string, downloadPort int, token string) (string, error) {
	// 如果 listenIP 为空，使用 0.0.0.0 监听所有接口
	actualListenIP := listenIP
	if actualListenIP == "" {
		actualListenIP = "0.0.0.0"
	}
	
	// 使用指定的端口，如果为 0 则系统自动分配
	listener, err := net.Listen("tcp", net.JoinHostPort(actualListenIP, fmt.Sprintf("%d", downloadPort)))
	if err != nil {
		return "", fmt.Errorf("failed to start TCP server on %s: %w", actualListenIP, err)
	}

	// 获取实际监听的地址
	downloadAddr := listener.Addr().String()
	
	// 如果原始 listenIP 不为空，使用它构建返回地址（而不是从 listener 获取）
	if listenIP != "" {
		if _, port, err := net.SplitHostPort(downloadAddr); err == nil {
			downloadAddr = net.JoinHostPort(listenIP, port)
		}
	}
	
	log.Printf("TCP download server started for token %s on %s", token, downloadAddr)

	go func() {
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept download connection failed: %v", err)
			return
		}
		defer conn.Close()

		if err := handleSingleDownload(conn, token); err != nil {
			log.Printf("handle download failed: %v", err)
		}
	}()

	return downloadAddr, nil
}

// handleSingleDownload 处理单个下载连接：校验 token，分片传输文件，实时报告进度
func handleSingleDownload(conn net.Conn, expectedToken string) error {
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

	// 2. 查找下载会话
	sess, ok := LookupDownloadSession(expectedToken)
	if !ok {
		return fmt.Errorf("download session not found or expired for token=%s", expectedToken)
	}

	// 3. 打开文件
	file, err := os.Open(sess.FilePath)
	if err != nil {
		return fmt.Errorf("open file failed: %w", err)
	}
	defer file.Close()

	// 4. 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat file failed: %w", err)
	}
	fileSize := fileInfo.Size()

	log.Printf("Starting download: file=%s, size=%d, path=%s", sess.FileName, fileSize, sess.FilePath)

	// 5. 发送文件元数据：文件大小
	header := fmt.Sprintf("%d\n", fileSize)
	if _, err := conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("send file size failed: %w", err)
	}

	// 6. 分片传输文件内容
	buffer := make([]byte, ChunkSize)
	var totalSent int64
	chunkIndex := 0

	for {
		// 读取一片数据
		n, err := file.Read(buffer)
		if n > 0 {
			// 发送这一片数据
			sent, writeErr := conn.Write(buffer[:n])
			if writeErr != nil {
				return fmt.Errorf("write chunk %d failed: %w", chunkIndex, writeErr)
			}

			totalSent += int64(sent)
			chunkIndex++

			// 计算并记录进度
			progress := float64(totalSent) / float64(fileSize) * 100
			log.Printf("Download progress: file=%s, chunk=%d, sent=%d/%d bytes (%.2f%%)",
				sess.FileName, chunkIndex, totalSent, fileSize, progress)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read file failed at chunk %d: %w", chunkIndex, err)
		}
	}

	if totalSent != fileSize {
		return fmt.Errorf("incomplete transfer: expected=%d, sent=%d", fileSize, totalSent)
	}

	log.Printf("Download completed: file=%s, size=%d, chunks=%d, token=%s",
		sess.FileName, totalSent, chunkIndex, expectedToken)

	// 7. 清理会话
	globalDownloadTokenStore.Delete(expectedToken)

	return nil
}

// HandleDownloadControlRequest 处理来自 Leader 的下载控制请求
// 这个函数在非 Leader 节点上运行，接收 Leader 的指令来启动下载服务器
func HandleDownloadControlRequest(conn net.Conn) error {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// 读取命令：START_DOWNLOAD
	line, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read command failed: %w", err)
	}

	command := strings.TrimSpace(line)
	if command != "START_DOWNLOAD" {
		return fmt.Errorf("invalid command: %s", command)
	}

	// 读取 token
	line, err = reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read token failed: %w", err)
	}
	token := strings.TrimSpace(line)

	// 读取文件名
	line, err = reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read filename failed: %w", err)
	}
	fileName := strings.TrimSpace(line)

	// 读取文件大小
	var fileSize int64
	if _, err := fmt.Fscanf(reader, "%d\n", &fileSize); err != nil {
		return fmt.Errorf("read file size failed: %w", err)
	}

	// 读取文件路径
	line, err = reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read file path failed: %w", err)
	}
	filePath := strings.TrimSpace(line)

	// 读取下载端口
	var downloadPort int
	if _, err := fmt.Fscanf(reader, "%d\n", &downloadPort); err != nil {
		return fmt.Errorf("read download port failed: %w", err)
	}

	log.Printf("Received download control request: token=%s, file=%s, size=%d, path=%s, port=%d",
		token, fileName, fileSize, filePath, downloadPort)

	// 创建下载会话
	sess := DownloadSession{
		FileName:  fileName,
		FileSize:  fileSize,
		FilePath:  filePath,
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	globalDownloadTokenStore.Put(token, sess)

	// 启动下载服务器
	downloadAddr, err := StartTCPDownloadServer("0.0.0.0", downloadPort, token)
	if err != nil {
		// 发送错误响应
		response := fmt.Sprintf("ERROR\n%s\n", err.Error())
		conn.Write([]byte(response))
		return fmt.Errorf("start download server failed: %w", err)
	}

	// 发送成功响应
	response := fmt.Sprintf("OK\n%s\n", downloadAddr)
	if _, err := conn.Write([]byte(response)); err != nil {
		return fmt.Errorf("send response failed: %w", err)
	}

	log.Printf("Download server started successfully: addr=%s", downloadAddr)
	return nil
}
