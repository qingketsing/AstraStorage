# MDS Store 说明

## 这份文档是干什么的

这份文档用比较容易理解的话，说明 `mds/store` 这一层现在负责什么、为什么要这样设计，以及你后面应该怎么继续写。

你可以把它理解成一句话：

`metadata` 负责定义“系统里有哪些数据”，`store` 负责定义“这些数据怎么保存、怎么更新、怎么保证一致性”。

---

## 现在这层代码放在哪里

核心代码在：

- [store.go](../../internal/mds/store/store.go)
- [txn.go](../../internal/mds/store/txn.go)

相关的数据结构在：

- [inode.go](../../internal/mds/metadata/inode.go)
- [object.go](../../internal/mds/metadata/object.go)
- [chunk.go](../../internal/mds/metadata/chunk.go)
- [replica.go](../../internal/mds/metadata/replica.go)

---

## 整体怎么理解

这套设计主要拆成 4 部分：

1. 目录树
   由 `inode` 负责。

2. 文件内容信息
   由 `FileMetadata` 负责。

3. 文件切片和副本
   由 `ChunkMetadata`、`ReplicaMetadata` 负责。

4. 上传过程
   由 `UploadSession` 负责。

也就是说：

- inode 解决“这个文件在目录树里的哪里”
- file 解决“这个文件本身是什么”
- chunk / replica 解决“这个文件被切成几片、存在哪些节点”
- upload session 解决“现在上传到哪里了、失败后怎么继续”

---

## 目录树怎么处理

目录树的核心是 `InodeMetadata`。

你可以把 inode 理解成文件系统里的“目录项节点”。
无论是目录还是文件，都先要有一个 inode。

它最重要的几个字段是：

- `ID`
  当前节点自己的唯一编号。

- `ParentID`
  当前节点的父目录是谁。

- `Name`
  当前节点在父目录里的名字。

- `Type`
  是 `file` 还是 `directory`。

- `Path`
  当前节点的完整路径，比如 `/docs/a.txt`。

这里最重要的理解是：

- 真正的目录树关系靠 `ID + ParentID + Name`
- `Path` 只是为了查询更方便，不是唯一真相

举个例子：

```text
/
├── docs
│   └── a.txt
└── images
```

可以理解成：

- `/` 是一个 inode
- `docs` 的 `ParentID` 指向 `/`
- `a.txt` 的 `ParentID` 指向 `docs`

所以后面做 rename 或 move 时：

- rename 主要改 `Name`
- move 主要改 `ParentID`
- `Path` 再跟着同步更新

---

## 文件信息怎么处理

文件本身的信息在 `FileMetadata` 里。

它主要描述：

- 这个文件的 ID
- 它对应哪个 inode
- 它的文件名和路径
- 它的大小
- 它的状态
- 它的 chunk 大小
- 它的校验信息
- 它分布在哪些节点上

这层不负责目录树关系本身，只负责“文件内容元数据”。

你可以这样分工：

- inode 关注树结构
- file 关注文件内容和存储状态

两者之间通过：

- `InodeID`
- `ParentInodeID`
- `Path`

这些字段保持关联。

---

## chunk 和副本怎么处理

一个文件上传后，不是整块直接存，而是会被切成很多 chunk。

当前设计里：

- 每一片 chunk 固定是 `4 MiB`
- 常量在 [object.go](../../internal/mds/metadata/object.go) 里：
  `FixedChunkSizeBytes = 4194304`

这意味着：

- 上传按 4 MiB 切片
- 下载按 4 MiB 定位分片
- 除最后一片外，所有 chunk 大小都应该一样

`ChunkMetadata` 主要描述：

- chunk 属于哪个文件
- 它是第几片
- 它的 offset 是多少
- 它的大小
- 它的状态
- 它的副本数
- 它有哪些副本

`ReplicaMetadata` 主要描述：

- 这片 chunk 存在哪个节点
- 它是主副本还是从副本
- 当前副本是否健康
- 副本自己的校验状态

这样设计的好处是：

- 某一片损坏时，可以只修这一片
- 某个副本坏了，不影响其他副本记录
- 以后做重平衡、补副本、迁移时，不需要重新设计

---

## 为什么是三副本

现在的设计明确假设：

- 一个文件正常情况下会有 3 份副本
- 默认期望副本数是 3
- 最小可读副本数现在设为 1

在代码里有这些结构：

- `ReplicaPolicy`
- `PrimaryNodeID`
- `SecondaryNodeIDs`
- `NodePlacements`

可以这样理解：

- `PrimaryNodeID`
  主节点是谁

- `SecondaryNodeIDs`
  另外两个副本节点是谁

- `NodePlacements`
  这个文件在每个节点上的详细状态

`NodePlacements` 不只是保存节点 ID，而是还可以挂：

- 是否主副本
- 这个节点上有哪些 chunk
- 当前副本状态
- 已存多大
- 最近同步时间

所以它比单纯的 `[]NodeID` 更适合后续扩展。

---

## 上传和续传怎么处理

上传过程单独放在 `UploadSession` 里。

这是因为：

- 文件本身是什么
- 这次上传进行到哪里了

这两件事不是一回事。

`UploadSession` 主要记录：

- 这次上传属于哪个文件
- 当前状态是什么
- 文件总大小是多少
- chunk 大小是多少
- 当前已经确认到哪个 offset
- 下次应该从哪个 offset 继续
- 最后成功落盘的是哪个 chunk
- 预期 checksum 是什么
- 实际校验结果是什么
- 失败了几次、要不要重试

这就能支持：

- 断点续传
- 校验失败后重传
- 记录失败位置
- 限制重试次数

---

## store 层到底负责什么

`store` 这一层不是数据库实现，而是数据库接口定义。

它的作用是：

- 先把“系统需要什么存储能力”说清楚
- 后面再用 PostgreSQL 去实现这些能力

现在主要有这些接口：

- `InodeRepository`
  处理目录树

- `FileRepository`
  处理文件元数据

- `ChunkRepository`
  处理 chunk 元数据

- `UploadSessionRepository`
  处理上传会话

- `NodeRepository`
  处理存储节点和心跳

- `TransactionManager`
  处理事务

- `HealthChecker`
  检查底层存储是否可用

这样拆开的好处是：

- 后面你可以分模块实现
- 不会所有逻辑都耦合在一个大类里
- 如果以后要换存储实现，业务代码不需要大改

---

## 目录树这层现在能做什么

`InodeRepository` 现在预留了这些操作：

- `CreateInode`
- `GetInode`
- `ListChildren`
- `UpdateInode`
- `MoveInode`
- `RenameInode`
- `DeleteInode`
- `UpdateSubtreePaths`

这些操作可以这样理解：

- `CreateInode`
  创建文件或目录节点

- `GetInode`
  按 ID、路径或者父目录加名字查节点

- `ListChildren`
  列出某个目录下面有哪些内容

- `UpdateInode`
  修改 inode 的普通属性

- `MoveInode`
  把文件或目录移到另一个目录下面

- `RenameInode`
  改名字

- `DeleteInode`
  删除一个 inode

- `UpdateSubtreePaths`
  当目录移动或重命名后，批量更新整棵子树的路径缓存

---

## 为什么要单独做事务

有些操作不能只做一半。

例如：

- 创建文件时，要同时创建 inode 和 file metadata
- 移动目录时，要同时更新 inode 和整棵子树路径
- 删除文件时，要同时删上传会话、chunk、副本记录

如果中间只成功一半，元数据就乱了。

所以这里单独定义了 `TransactionManager` 和 `Tx`。

它的目标很简单：

- 这些必须一起成功的操作，后面都放到一个事务里

---

## 你后面应该怎么继续写

建议顺序是：

1. 先看 [mds-invariants.md](./mds-invariants.md)
   先把必须长期成立的规则确认下来。

2. 再设计 PostgreSQL 表结构
   至少会需要：
   - `inodes`
   - `files`
   - `chunks`
   - `chunk_replicas`
   - `upload_sessions`
   - `storage_nodes`

3. 先实现最小链路
   先做：
   - `CreateInode`
   - `CreateFile`
   - `CreateUploadSession`
   - `UpsertChunks`
   - `GetFile` / `GetInode`

4. 再补 rename、move、副本修复、重试这些复杂操作

---

## 一句话总结

现在这套 `store` 设计已经把最核心的事情分清楚了：

- 目录树怎么管
- 文件怎么管
- chunk 怎么管
- 副本怎么管
- 上传过程怎么管
- 哪些操作必须事务化

它还不是完整实现，但作为后续接 PostgreSQL 和继续写服务逻辑的基础，已经够用了。
