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
