// inode.go
// Metadata Service 中 inode 元信息模型定义文件。
// 该文件预留用于描述文件或目录的核心元数据结构，
// 包括标识信息、权限属性、层级关系、时间戳以及与数据块的关联关系。

package metadata

import "time"

const RootInodeID = "root"

// InodeType 描述 inode 的对象类型。
type InodeType string

const (
	InodeTypeFile      InodeType = "file"
	InodeTypeDirectory InodeType = "directory"
)

// InodeStatus 描述 inode 当前的生命周期状态。
type InodeStatus string

const (
	InodeStatusActive   InodeStatus = "active"
	InodeStatusDeleting InodeStatus = "deleting"
	InodeStatusDeleted  InodeStatus = "deleted"
)

// InodeID 是 inode 的全局唯一标识。
type InodeID string

// PathSegment 表示路径中的单个名称片段。
type PathSegment string

// DirectoryEntry 表示目录中的一个子项。
type DirectoryEntry struct {
	ParentID  InodeID
	ChildID   InodeID
	Name      string
	Type      InodeType
	CreatedAt time.Time
	UpdatedAt time.Time
}

// TreePath 保存目录树路径解析的结构化结果。
type TreePath struct {
	Namespace string
	Raw       string
	Segments  []PathSegment
	Depth     int
}

// InodeMetadata 描述文件系统风格命名空间中的 inode 元数据。
type InodeMetadata struct {
	ID          InodeID
	ParentID    InodeID
	FileID      FileID
	Path        string
	Name        string
	Type        InodeType
	Status      InodeStatus
	Size        int64
	Permissions uint32
	Owner       string
	Group       string
	LinkCount   int64
	Generation  int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	AccessedAt  *time.Time
}
