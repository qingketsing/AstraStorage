package upload

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
)

// StartTCPUploadServer 为一次上传请求临时启动一个 TCP 监听
// listenIP 是当前节点对外可达的 IP（例如 "127.0.0.1" 或 内网 IP）
// token 用于在第一步校验连接是否合法
// 返回值 uploadAddr 是客户端应当使用的 "ip:port" 地址
func StartTCPUploadServer(listenIP, token string) (string, error) {
	// 端口使用 0，让系统自动分配可用端口
	listener, err := net.Listen("tcp", net.JoinHostPort(listenIP, "0"))
	if err != nil {
		return "", fmt.Errorf("failed to start TCP server on %s: %w", listenIP, err)
	}

	uploadAddr := listener.Addr().String()
	log.Printf("TCP upload server started for token %s on %s", token, uploadAddr)

	go func() {
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("accept upload connection failed: %v", err)
			return
		}
		defer conn.Close()

		if err := handleSingleUpload(conn, token); err != nil {
			log.Printf("handle upload failed: %v", err)
		}
	}()

	return uploadAddr, nil
}

// 处理单个上传连接：校验 token，按会话信息接收文件
func handleSingleUpload(conn net.Conn, expectedToken string) error {
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

	// 2. 查找上传会话
	sess, ok := LookupUploadSession(expectedToken)
	if !ok {
		return fmt.Errorf("upload session not found or expired for token=%s", expectedToken)
	}

	// 3. 将接收的数据保存在本地 FileStorage 目录
	if err := os.MkdirAll("FileStorage", 0o755); err != nil {
		return fmt.Errorf("create FileStorage dir failed: %w", err)
	}
	filePath := filepath.Join("FileStorage", sess.FileName)
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create file failed: %w", err)
	}
	defer file.Close()

	limitReader := io.LimitReader(reader, sess.FileSize)
	written, err := io.Copy(file, limitReader)
	if err != nil {
		return fmt.Errorf("receive file data failed: %w", err)
	}
	if written != sess.FileSize {
		return fmt.Errorf("incomplete upload: expect=%d, got=%d", sess.FileSize, written)
	}

	log.Printf("file upload finished: path=%s, name=%s, size=%d, token=%s", filePath, sess.FileName, written, expectedToken)
	// 这里之后可以触发: 写 PostgreSQL + 复制到其他节点 + 更新两边数据库
	return nil
}
