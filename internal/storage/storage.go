package storage

// 导出类型，包外可见
type MetaData struct {
	id       int64  // 私有字段，包外不可见
	Name     string `json:"name"`
	Capacity int64  `json:"capacity"`
}

type FileObject struct {
	Name       string `json:"name"`
	Capacity   int64  `json:"capacity"`
	Content    []byte `json:"content"`
	StorageAdd string `json:"storageAdd"`
}

// storageAdd 表示这个文件的目录树，比如1-2-3，那么就是1作为根文件夹，2作为3的父文件夹，3为当前文件的id
// 这些数字都是代表着id
// 同时，数据库记录了存储着这个文件的存储所有节点ID列表，以逗号分隔
// 在上传文件时，客户端会上传到某个节点，然后该节点负责将文件复制到其他节点，确保冗余备份
// 在删除文件时，客户端只需请求一个节点删除，节点会负责通知其他节点删除该文件，确保一致性
