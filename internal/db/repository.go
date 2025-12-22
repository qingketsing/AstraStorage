// 这个文件是数据库的各种操作，包括增删改查等功能的实现。
package db

import (
	"fmt"
	"log"
	"time"
)

// FileUploadInfo 包含文件上传后需要保存到数据库的信息
type FileUploadInfo struct {
	FileName     string    // 文件名
	FileSize     int64     // 文件大小（字节）
	LocalPath    string    // 本地存储路径（为空表示本节点没有存储文件，仅有元数据）
	StorageNodes string    // 存储节点ID列表（逗号分隔）
	StorageAdd   string    // 文件目录树路径（格式: "1-2-3"）
	OwnerID      string    // 文件所有者ID（可选）
	CreatedAt    time.Time // 创建时间
}

// SaveFileUpload 保存文件上传信息到数据库
// 返回文件ID（数据库自增主键）
// 注意：local_path 可以为空，表示本节点只有元数据，不存储实际文件
func (dbc *DBConnection) SaveFileUpload(info FileUploadInfo) (int64, error) {
	if info.CreatedAt.IsZero() {
		info.CreatedAt = time.Now()
	}

	// 如果 local_path 为空，使用 NULL
	var localPath interface{}
	if info.LocalPath == "" {
		localPath = nil
	} else {
		localPath = info.LocalPath
	}

	query := `
		INSERT INTO files (file_name, file_size, local_path, storage_nodes, storage_add, owner_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	var fileID int64
	err := dbc.db.QueryRow(
		query,
		info.FileName,
		info.FileSize,
		localPath,
		info.StorageNodes,
		info.StorageAdd,
		info.OwnerID,
		info.CreatedAt,
	).Scan(&fileID)

	if err != nil {
		return 0, fmt.Errorf("save file upload failed: %w", err)
	}

	return fileID, nil
}

func (dbc *DBConnection) InsertFileRecord(fileID string, ownerID string, storageNodes string) error {
	// 实现插入文件记录的逻辑
	query := `INSERT INTO files (file_id, owner_id, storage_nodes, created_at) VALUES ($1, $2, $3, NOW())`
	_, err := dbc.db.Exec(query, fileID, ownerID, storageNodes)
	if err != nil {
		return fmt.Errorf("insert file record failed: %w", err)
	}
	return nil
}

func (dbc *DBConnection) GetFileRecord() (string, string, error) {
	// 实现获取文件记录的逻辑
	return "", "", nil
}

func (dbc *DBConnection) DeleteFileRecord() error {
	// 实现删除文件记录的逻辑
	return nil
}

func (dbc *DBConnection) UpdateFileRecord() error {
	// 实现更新文件记录的逻辑
	return nil
}

// UpdateFileStorageNodes 更新文件的存储节点列表
func (dbc *DBConnection) UpdateFileStorageNodes(fileID int64, storageNodes string) error {
	query := `UPDATE files SET storage_nodes = $1, updated_at = NOW() WHERE id = $2`
	_, err := dbc.db.Exec(query, storageNodes, fileID)
	if err != nil {
		return fmt.Errorf("update storage nodes failed: %w", err)
	}
	return nil
}

// GetFileByID 根据文件ID获取文件信息
func (dbc *DBConnection) GetFileByID(fileID int64) (*FileInfo, error) {
	query := `
		SELECT id, file_name, file_size, local_path, storage_nodes, storage_add, owner_id, created_at
		FROM files
		WHERE id = $1
	`
	var info FileInfo
	var localPath *string
	err := dbc.db.QueryRow(query, fileID).Scan(
		&info.ID,
		&info.FileName,
		&info.FileSize,
		&localPath,
		&info.StorageNodes,
		&info.StorageAdd,
		&info.OwnerID,
		&info.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get file by id failed: %w", err)
	}
	if localPath != nil {
		info.LocalPath = *localPath
	}
	return &info, nil
}

// FileInfo 文件信息结构
type FileInfo struct {
	ID           int64
	FileName     string
	FileSize     int64
	LocalPath    string
	StorageNodes string
	StorageAdd   string
	OwnerID      string
	CreatedAt    time.Time
}

// === 目录树管理 ===

// DirectoryNode 目录节点结构
type DirectoryNode struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	ParentID  *int64    `json:"parent_id"` // NULL表示根目录
	IsDir     bool      `json:"is_dir"`
	FileID    *int64    `json:"file_id"` // 如果是文件节点，关联到files表的id
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateDirectory 创建目录
func (dbc *DBConnection) CreateDirectory(name string, parentID *int64, ownerID string) (int64, error) {
	query := `
		INSERT INTO directory_tree (name, parent_id, is_dir, owner_id, created_at)
		VALUES ($1, $2, true, $3, NOW())
		RETURNING id
	`
	var dirID int64
	err := dbc.db.QueryRow(query, name, parentID, ownerID).Scan(&dirID)
	if err != nil {
		return 0, fmt.Errorf("create directory failed: %w", err)
	}
	return dirID, nil
}

// AddFileToDirectory 将文件添加到目录树
func (dbc *DBConnection) AddFileToDirectory(fileName string, fileID int64, parentID *int64, ownerID string) (int64, error) {
	query := `
		INSERT INTO directory_tree (name, parent_id, is_dir, file_id, owner_id, created_at)
		VALUES ($1, $2, false, $3, $4, NOW())
		RETURNING id
	`
	var nodeID int64
	err := dbc.db.QueryRow(query, fileName, parentID, fileID, ownerID).Scan(&nodeID)
	if err != nil {
		return 0, fmt.Errorf("add file to directory failed: %w", err)
	}

	// 更新files表的storage_add字段
	storageAdd, err := dbc.buildStoragePath(nodeID)
	if err != nil {
		return nodeID, nil // 忽略路径构建错误
	}

	if err := dbc.UpdateFileStorageAdd(fileID, storageAdd); err != nil {
		log.Printf("update storage_add failed: %v", err)
	}

	return nodeID, nil
}

// UpdateFileStorageAdd 更新文件的目录树路径
func (dbc *DBConnection) UpdateFileStorageAdd(fileID int64, storageAdd string) error {
	query := `UPDATE files SET storage_add = $1 WHERE id = $2`
	_, err := dbc.db.Exec(query, storageAdd, fileID)
	if err != nil {
		return fmt.Errorf("update storage_add failed: %w", err)
	}
	return nil
}

// buildStoragePath 构建文件的目录树路径（格式：1-2-3）
func (dbc *DBConnection) buildStoragePath(nodeID int64) (string, error) {
	query := `
		WITH RECURSIVE path AS (
			SELECT id, parent_id, CAST(id AS TEXT) as path
			FROM directory_tree
			WHERE id = $1
			UNION ALL
			SELECT dt.id, dt.parent_id, CAST(dt.id AS TEXT) || '-' || p.path
			FROM directory_tree dt
			INNER JOIN path p ON dt.id = p.parent_id
		)
		SELECT path FROM path WHERE parent_id IS NULL
	`
	var path string
	err := dbc.db.QueryRow(query, nodeID).Scan(&path)
	if err != nil {
		return "", fmt.Errorf("build storage path failed: %w", err)
	}
	return path, nil
}

// ListDirectory 列出目录下的所有子项
func (dbc *DBConnection) ListDirectory(parentID *int64) ([]*DirectoryNode, error) {
	query := `
		SELECT id, name, parent_id, is_dir, file_id, owner_id, created_at
		FROM directory_tree
		WHERE parent_id IS NOT DISTINCT FROM $1
		ORDER BY is_dir DESC, name ASC
	`

	rows, err := dbc.db.Query(query, parentID)
	if err != nil {
		return nil, fmt.Errorf("list directory failed: %w", err)
	}
	defer rows.Close()

	nodes := make([]*DirectoryNode, 0)
	for rows.Next() {
		var node DirectoryNode
		err := rows.Scan(
			&node.ID,
			&node.Name,
			&node.ParentID,
			&node.IsDir,
			&node.FileID,
			&node.OwnerID,
			&node.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan directory node failed: %w", err)
		}
		nodes = append(nodes, &node)
	}

	return nodes, nil
}

// MoveNode 移动文件或目录到新的父目录
func (dbc *DBConnection) MoveNode(nodeID int64, newParentID *int64) error {
	query := `UPDATE directory_tree SET parent_id = $1 WHERE id = $2`
	_, err := dbc.db.Exec(query, newParentID, nodeID)
	if err != nil {
		return fmt.Errorf("move node failed: %w", err)
	}

	// 如果是文件节点，更新storage_add
	var fileID *int64
	err = dbc.db.QueryRow(`SELECT file_id FROM directory_tree WHERE id = $1`, nodeID).Scan(&fileID)
	if err == nil && fileID != nil {
		if storageAdd, err := dbc.buildStoragePath(nodeID); err == nil {
			dbc.UpdateFileStorageAdd(*fileID, storageAdd)
		}
	}

	return nil
}

// DeleteNode 删除目录节点（及其子节点）
func (dbc *DBConnection) DeleteNode(nodeID int64) error {
	// 使用递归删除
	query := `
		WITH RECURSIVE subtree AS (
			SELECT id FROM directory_tree WHERE id = $1
			UNION ALL
			SELECT dt.id FROM directory_tree dt
			INNER JOIN subtree s ON dt.parent_id = s.id
		)
		DELETE FROM directory_tree WHERE id IN (SELECT id FROM subtree)
	`
	_, err := dbc.db.Exec(query, nodeID)
	if err != nil {
		return fmt.Errorf("delete node failed: %w", err)
	}
	return nil
}

// === 元数据管理辅助方法 ===

// GetFileByName 根据文件名获取文件信息
func (dbc *DBConnection) GetFileByName(fileName string) (*FileInfo, error) {
	query := `
		SELECT id, file_name, file_size, local_path, storage_nodes, storage_add, owner_id, created_at
		FROM files
		WHERE file_name = $1
		ORDER BY created_at DESC
		LIMIT 1
	`
	var info FileInfo
	var localPath *string
	err := dbc.db.QueryRow(query, fileName).Scan(
		&info.ID,
		&info.FileName,
		&info.FileSize,
		&localPath,
		&info.StorageNodes,
		&info.StorageAdd,
		&info.OwnerID,
		&info.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get file by name failed: %w", err)
	}
	if localPath != nil {
		info.LocalPath = *localPath
	}
	return &info, nil
}

// HasFileLocally 检查本节点是否存储了指定文件
func (dbc *DBConnection) HasFileLocally(fileID int64) (bool, string, error) {
	query := `SELECT local_path FROM files WHERE id = $1`
	var localPath *string
	err := dbc.db.QueryRow(query, fileID).Scan(&localPath)
	if err != nil {
		return false, "", fmt.Errorf("query file failed: %w", err)
	}
	if localPath != nil && *localPath != "" {
		return true, *localPath, nil
	}
	return false, "", nil
}

// ListAllFiles 列出所有文件（包括只有元数据的）
func (dbc *DBConnection) ListAllFiles() ([]*FileInfo, error) {
	query := `
		SELECT id, file_name, file_size, local_path, storage_nodes, storage_add, owner_id, created_at
		FROM files
		ORDER BY created_at DESC
	`
	rows, err := dbc.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query files failed: %w", err)
	}
	defer rows.Close()

	var files []*FileInfo
	for rows.Next() {
		var info FileInfo
		var localPath *string
		err := rows.Scan(
			&info.ID,
			&info.FileName,
			&info.FileSize,
			&localPath,
			&info.StorageNodes,
			&info.StorageAdd,
			&info.OwnerID,
			&info.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan file row failed: %w", err)
		}
		if localPath != nil {
			info.LocalPath = *localPath
		}
		files = append(files, &info)
	}
	return files, nil
}

// ListFilesWithLocalStorage 列出本节点实际存储的文件
func (dbc *DBConnection) ListFilesWithLocalStorage() ([]*FileInfo, error) {
	query := `
		SELECT id, file_name, file_size, local_path, storage_nodes, storage_add, owner_id, created_at
		FROM files
		WHERE local_path IS NOT NULL AND local_path != ''
		ORDER BY created_at DESC
	`
	rows, err := dbc.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query files failed: %w", err)
	}
	defer rows.Close()

	var files []*FileInfo
	for rows.Next() {
		var info FileInfo
		err := rows.Scan(
			&info.ID,
			&info.FileName,
			&info.FileSize,
			&info.LocalPath,
			&info.StorageNodes,
			&info.StorageAdd,
			&info.OwnerID,
			&info.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan file row failed: %w", err)
		}
		files = append(files, &info)
	}
	return files, nil
}

// === 文件删除操作 ===

// DeleteFileByName 根据文件名删除文件记录
func (dbc *DBConnection) DeleteFileByName(fileName string) error {
	query := `DELETE FROM files WHERE file_name = $1`
	result, err := dbc.db.Exec(query, fileName)
	if err != nil {
		return fmt.Errorf("delete file by name failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected failed: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("file not found: %s", fileName)
	}

	log.Printf("Deleted file record: %s, rows affected: %d", fileName, rowsAffected)
	return nil
}

// DeleteFileByID 根据文件ID删除文件记录
func (dbc *DBConnection) DeleteFileByID(fileID int64) error {
	query := `DELETE FROM files WHERE id = $1`
	result, err := dbc.db.Exec(query, fileID)
	if err != nil {
		return fmt.Errorf("delete file by id failed: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected failed: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("file not found with id: %d", fileID)
	}

	log.Printf("Deleted file record with id: %d, rows affected: %d", fileID, rowsAffected)
	return nil
}
