package tests

import (
	"database/sql"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestMetadataSyncToAllNodes 测试文件元数据是否同步到所有节点
// 验证：所有5个数据库节点都有文件元数据，但只有2个节点实际存储文件
func TestMetadataSyncToAllNodes(t *testing.T) {
	log.SetOutput(os.Stdout)

	t.Log("=== 文件元数据同步测试 ===")

	// 步骤1: 检查Docker集群状态
	t.Log("\n[步骤1] 检查 Docker 集群状态...")
	if err := checkDockerCluster(t); err != nil {
		t.Fatalf("Docker 集群未就绪: %v\n请先运行: .\\scripts\\start_docker_cluster.ps1", err)
	}
	t.Log("✓ Docker 集群运行正常")

	// 步骤2: 初始化数据库表
	t.Log("\n[步骤2] 初始化数据库表结构...")
	if err := initDatabaseSchema(t); err != nil {
		t.Fatalf("数据库初始化失败: %v", err)
	}
	t.Log("✓ 数据库表结构已创建")

	// 步骤3: 创建测试文件
	t.Log("\n[步骤3] 创建测试文件...")
	testFile := createTestFile(t)
	defer os.Remove(testFile)
	t.Logf("✓ 测试文件已创建: %s (大小: %d 字节)", testFile, len(TestFileContent))

	// 步骤4: 通过RabbitMQ请求上传地址
	t.Log("\n[步骤4] 连接 RabbitMQ 并请求上传地址...")
	uploadReply, err := requestUploadAddress(t, testFile)
	if err != nil {
		t.Fatalf("请求上传地址失败: %v", err)
	}
	t.Logf("✓ 获得上传地址: %s, Token: %s", uploadReply.UploadAddr, uploadReply.Token)

	// 步骤5: 上传文件
	t.Log("\n[步骤5] 通过 TCP 上传文件...")
	if err := uploadFileViaTCP(t, testFile, uploadReply.UploadAddr, uploadReply.Token); err != nil {
		t.Fatalf("文件上传失败: %v", err)
	}
	t.Log("✓ 文件上传成功")

	// 步骤6: 等待文件复制和元数据同步
	t.Log("\n[步骤6] 等待文件复制和元数据同步...")
	time.Sleep(5 * time.Second)
	t.Log("✓ 等待完成")

	// 步骤7: 验证所有数据库节点都有元数据记录
	t.Log("\n[步骤7] 验证所有5个数据库节点的元数据...")
	storageNodes, results := verifyAllDatabasesHaveMetadata(t, TestFileName)

	// 步骤8: 输出结果
	t.Log("\n=== 测试结果汇总 ===")
	t.Log("✓ 文件上传成功")
	t.Logf("✓ 文件实际存储在 %d 个节点: %v", len(storageNodes), storageNodes)
	t.Logf("✓ 所有 5 个数据库节点都有元数据记录")

	nodesWithFile := 0
	nodesWithMetadataOnly := 0
	for i, result := range results {
		if result.HasFile {
			nodesWithFile++
			t.Logf("  - Node %d (port %d): 有元数据 且 存储文件 (local_path='%s') ✓",
				i, 20000+i, result.LocalPath)
		} else {
			nodesWithMetadataOnly++
			t.Logf("  - Node %d (port %d): 有元数据 但 不存储文件 (local_path='') ✓", i, 20000+i)
		}
	}

	t.Logf("✓ 统计: %d个节点存储文件, %d个节点只有元数据", nodesWithFile, nodesWithMetadataOnly)

	// 验证预期：应该有2个节点存储文件，3个节点只有元数据
	if nodesWithFile != 2 {
		t.Errorf("❌ 错误: 预期2个节点存储文件，实际%d个", nodesWithFile)
	}
	if nodesWithMetadataOnly != 3 {
		t.Errorf("❌ 错误: 预期3个节点只有元数据，实际%d个", nodesWithMetadataOnly)
	}

	t.Log("✓ 元数据同步正确")
	t.Log("\n🎉 所有测试通过！")
}

// MetadataResult 表示每个节点的元数据状态
type MetadataResult struct {
	NodeIndex    int
	Port         int
	HasMetadata  bool
	HasFile      bool // local_path 不为空表示有文件
	LocalPath    string
	FileName     string
	FileSize     int64
	StorageNodes string
}

// verifyAllDatabasesHaveMetadata 验证所有5个数据库节点都有元数据
func verifyAllDatabasesHaveMetadata(t *testing.T, fileName string) ([]string, []MetadataResult) {
	results := make([]MetadataResult, 5)
	storageNodesSet := make(map[string]bool)
	var storageNodesStr string

	dsns := []string{
		PostgresNode0DSN,
		PostgresNode1DSN,
		PostgresNode2DSN,
		PostgresNode3DSN,
		PostgresNode4DSN,
	}

	for i := 0; i < 5; i++ {
		port := 20000 + i
		result := MetadataResult{
			NodeIndex: i,
			Port:      port,
		}

		// 连接数据库
		db, err := sql.Open("postgres", dsns[i])
		if err != nil {
			t.Logf("  ⚠ 连接 Node %d (port %d) 失败: %v", i, port, err)
			results[i] = result
			continue
		}
		defer db.Close()

		// 查询文件记录
		var id int64
		var localPath sql.NullString // 修改为 NullString 以处理 NULL 值
		var recordFileName string
		var recordFileSize int64
		var recordStorageNodes string

		query := `SELECT id, file_name, file_size, local_path, storage_nodes 
		          FROM files WHERE file_name = $1 ORDER BY id DESC LIMIT 1`
		err = db.QueryRow(query, fileName).Scan(&id, &recordFileName, &recordFileSize, &localPath, &recordStorageNodes)

		if err == sql.ErrNoRows {
			t.Errorf("  ✗ Node %d (port %d): 没有找到文件记录！", i, port)
			result.HasMetadata = false
		} else if err != nil {
			t.Errorf("  ✗ Node %d (port %d): 查询失败: %v", i, port, err)
			result.HasMetadata = false
		} else {
			result.HasMetadata = true
			result.FileName = recordFileName
			result.FileSize = recordFileSize
			result.StorageNodes = recordStorageNodes

			// 处理 NULL 值
			if localPath.Valid {
				result.LocalPath = localPath.String
				result.HasFile = (localPath.String != "")
			} else {
				result.LocalPath = ""
				result.HasFile = false
			}

			if storageNodesStr == "" {
				storageNodesStr = recordStorageNodes
			}

			t.Logf("  ✓ Node %d (port %d): 找到记录 (id=%d, local_path='%s', storage_nodes=%s)",
				i, port, id, result.LocalPath, recordStorageNodes)
		}

		results[i] = result
	}

	// 检查是否所有节点都有元数据
	allHaveMetadata := true
	for _, result := range results {
		if !result.HasMetadata {
			allHaveMetadata = false
			break
		}
	}

	if !allHaveMetadata {
		t.Fatal("❌ 不是所有节点都有元数据记录！")
	}

	// 解析 storage_nodes
	if storageNodesStr != "" {
		nodes := strings.Split(storageNodesStr, ",")
		for _, node := range nodes {
			storageNodesSet[strings.TrimSpace(node)] = true
		}
	}

	storageNodes := make([]string, 0, len(storageNodesSet))
	for node := range storageNodesSet {
		storageNodes = append(storageNodes, node)
	}

	return storageNodes, results
}
