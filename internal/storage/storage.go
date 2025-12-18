package storage

import "time"

// 导出类型，包外可见
type MetaData struct {
	ID       int64  `json:"id"`       // 文件ID，数据库主键
	Name     string `json:"name"`     // 文件名称
	Capacity int64  `json:"capacity"` // 文件大小（字节）
}

type FileObject struct {
	ID           int64     `json:"id"`           // 文件ID
	Name         string    `json:"name"`         // 文件名称
	Capacity     int64     `json:"capacity"`     // 文件大小（字节）
	Content      []byte    `json:"content"`      // 文件内容（可选，用于小文件）
	StorageAdd   string    `json:"storageAdd"`   // 文件目录树路径，格式: "1-2-3"
	StorageNodes string    `json:"storageNodes"` // 存储该文件的节点ID列表，逗号分隔
	CreatedAt    time.Time `json:"createdAt"`    // 创建时间
	LocalPath    string    `json:"localPath"`    // 本地存储路径
}

// storageAdd 表示这个文件的目录树，比如1-2-3，那么就是1作为根文件夹，2作为3的父文件夹，3为当前文件的id
// 这些数字都是代表着id
// 同时，数据库记录了存储着这个文件的存储所有节点ID列表，以逗号分隔
// 在上传文件时，客户端会上传到某个节点，然后该节点负责将文件复制到其他节点，确保冗余备份
// 在删除文件时，客户端只需请求一个节点删除，节点会负责通知其他节点删除该文件，确保一致性
