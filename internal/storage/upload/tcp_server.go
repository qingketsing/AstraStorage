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
	"time"

	"multi_driver/internal/core/replication"
	"multi_driver/internal/db"
)

// UploadContext 上传上下文，包含文件复制所需的所有信息
type UploadContext struct {
	NodeID         string
	DB             *db.DBConnection
	ReplicationMgr *replication.ReplicationManager
}

// StartTCPUploadServer 为一次上传请求临时启动一个 TCP 监听
// listenIP 是当前节点对外可达的 IP（例如 "127.0.0.1" 或 内网 IP）
// uploadPort 是指定的上传端口（0 表示系统自动分配）
// token 用于在第一步校验连接是否合法
// ttl 指定了监听器的最大生存时间，如果在此时间内无需连接，监听器将关闭
// 返回值 uploadAddr 是客户端应当使用的 "ip:port" 地址
func StartTCPUploadServer(listenIP string, uploadPort int, token string, ttl time.Duration, ctx *UploadContext) (string, error) {
	// 使用指定的端口，如果为 0 则系统自动分配
	listener, err := net.Listen("tcp", net.JoinHostPort(listenIP, fmt.Sprintf("%d", uploadPort)))
	if err != nil {
		return "", fmt.Errorf("failed to start TCP server on %s: %w", listenIP, err)
	}

	uploadAddr := listener.Addr().String()
	log.Printf("TCP upload server started for token %s on %s with TTL %v", token, uploadAddr, ttl)

	go func() {
		defer listener.Close()

		// 设置 Deadline，防止永久阻塞
		if tcpListener, ok := listener.(*net.TCPListener); ok {
			_ = tcpListener.SetDeadline(time.Now().Add(ttl))
		}

		conn, err := listener.Accept()
		if err != nil {
			// 如果是超时错误，这是预期的行为（清理资源）
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				log.Printf("upload listener for token %s timed out (resource cleaned up)", token)
			} else {
				log.Printf("accept upload connection failed: %v", err)
			}
			// 移除过期的 token，虽然 TokenStore.Get 会检查，但主动删除更干净
			globalTokenStore.Delete(token)
			return
		}
		defer conn.Close()

		if err := handleSingleUpload(conn, token, ctx); err != nil { // 文件上传处理
			log.Printf("handle upload failed: %v", err)
		}
	}()

	return uploadAddr, nil
}

// 处理单个上传连接：校验 token，按会话信息接收文件
func handleSingleUpload(conn net.Conn, expectedToken string, ctx *UploadContext) error {
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

	log.Printf("[%s] file upload finished: path=%s, name=%s, size=%d, token=%s", ctx.NodeID, filePath, sess.FileName, written, expectedToken)

	// 保存文件信息到数据库
	var fileID int64
	if ctx.DB != nil {
		fileInfo := db.FileUploadInfo{
			FileName:     sess.FileName,
			FileSize:     written,
			LocalPath:    filePath,
			StorageNodes: ctx.NodeID, // 初始只有当前节点
			StorageAdd:   "",         // 默认根目录
			OwnerID:      sess.ClientIP,
		}

		var err error
		fileID, err = ctx.DB.SaveFileUpload(fileInfo)
		if err != nil {
			log.Printf("[%s] save file to database failed: %v", ctx.NodeID, err)
			// 不返回错误，文件已经保存到本地，数据库写入失败可以后续补救
		} else {
			log.Printf("[%s] file saved to database: id=%d, name=%s, size=%d", ctx.NodeID, fileID, sess.FileName, written)
		}
	}

	// 复制文件到其他节点
	if ctx.ReplicationMgr != nil {
		log.Printf("[%s] starting file replication for %s", ctx.NodeID, sess.FileName)
		successNodes, err := ctx.ReplicationMgr.ReplicateFile(filePath, sess.FileName, written)
		if err != nil {
			log.Printf("[%s] file replication failed: %v", ctx.NodeID, err)
		} else {
			log.Printf("[%s] file replicated to %d nodes: %v", ctx.NodeID, len(successNodes), successNodes)

			// 更新数据库中的 storage_nodes 字段
			if ctx.DB != nil && fileID > 0 {
				storageNodesStr := strings.Join(successNodes, ",")
				if err := ctx.DB.UpdateFileStorageNodes(fileID, storageNodesStr); err != nil {
					log.Printf("[%s] update storage nodes failed: %v", ctx.NodeID, err)
				} else {
					log.Printf("[%s] updated storage_nodes in database: %s", ctx.NodeID, storageNodesStr)
				}

				// 向所有其他节点广播元数据（包括那些不存储文件的节点）
				log.Printf("[%s] broadcasting file metadata to all nodes", ctx.NodeID)
				if err := ctx.ReplicationMgr.BroadcastMetadata(sess.FileName, written, successNodes, sess.ClientIP); err != nil {
					log.Printf("[%s] metadata broadcast failed: %v", ctx.NodeID, err)
				} else {
					log.Printf("[%s] metadata broadcast completed", ctx.NodeID)
				}
			}
		}
	} else {
		log.Printf("[%s] replication manager not available, file stored locally only", ctx.NodeID)
	}

	return nil
}
