# MDS 读写链路说明

## 这份文档是干什么的

这份文档说明当前仓库里已经实现的两条主链路：

- 写路径：从创建目录到完成上传
- 读路径：从查询元数据到生成下载计划

对应代码主要在：

- [service_directory.go](../../internal/mds/service_directory.go)
- [service_file.go](../../internal/mds/service_file.go)
- [service_upload.go](../../internal/mds/service_upload.go)
- [service_read.go](../../internal/mds/service_read.go)
- [service_test.go](../../internal/mds/service_test.go)

---

## 当前写路径

当前最完整的写路径是：

`CreateDirectory -> CreateFile -> StartUpload -> CommitChunk -> CompleteUpload -> VerifyUpload`

在校验失败分支上，当前还支持：

`CompleteUpload -> FailUploadVerification -> RetryUpload -> CommitChunk ...`

### 1. CreateDirectory

负责：

- 校验目录创建请求
- 读取父目录
- 生成目录 inode
- 在事务里写入 inode

这一步只动目录树，不创建 file 记录。

### 2. CreateFile

负责：

- 校验文件创建请求
- 读取父目录 inode
- 创建文件型 inode
- 创建 `FileMetadata`
- 在同一个事务里完成 inode 和 file 的双写

这一步是当前最基础的“跨表原子操作”。

### 3. StartUpload

负责：

- 读取目标文件
- 创建 `UploadSession`
- 把文件状态推进到 `uploading`
- 回写 `LatestUploadSessionID`
- 拒绝同一文件并发存在多个未终结会话

这一步把“文件对象”和“一次上传过程”连接起来。

### 4. CommitChunk

负责：

- 校验上传会话可继续写入
- 创建或更新 chunk 元数据
- 更新 upload session 的 offset
- 更新 `LastPersistedChunk`
- 重新计算 `file.StoredSize`

这一步是当前上传过程中最重要的状态推进点。

### 5. CompleteUpload

负责：

- 校验上传会话是否可完成
- 校验 chunk 覆盖完整、顺序连续
- 把 upload session 推进到 `verifying`
- 把所有 chunk 推进到 `verifying`
- 把文件状态推进到 `verifying`
- 确认最终 `StoredSize`

### 6. VerifyUpload

负责：

- 校验每个 chunk 都具备最小可读副本
- 校验每个 chunk 的 checksum 已验证
- 校验文件最终 checksum 已验证
- 完成 upload session
- 把所有 chunk 推进到 `available`
- 把文件状态推进到 `available`
- 回写 `CompletedAt`

这一步表示“写路径完成，文件进入可读状态”。

### 7. FailUploadVerification

负责：

- 记录一次 verifier 发现的失败现场
- 把 upload session 推进到 `failed` 或 `retrying`
- 把文件推进到 `failed`
- 把 chunk 推进到 `failed`
- 记录失败 offset、失败 chunk 和重试元数据

这一步表示“写入已经封口，但校验没有通过”。

### 8. RetryUpload

负责：

- 只接受 `retrying` 状态的 upload session
- 保留失败点之前的已持久化 chunk
- 删除失败点及之后的 chunk
- 清空旧的 verified checksum
- 把 upload session 重新推进到 `active`
- 把文件重新推进到 `uploading`

这一步表示“基于失败现场重新打开上传窗口”，而不是重新创建一个全新的文件对象。

---

## 当前读路径

当前已经支持的读路径包括：

- `GetInode`
- `GetFile`
- `GetUploadSession`
- `ListChildren`
- `ListFileChunks`
- `BuildDownloadPlan`

### 1. GetInode / GetFile / GetUploadSession

这三个接口分别返回：

- 目录树节点
- 文件对象
- 上传过程状态

它们是最基础的元数据查询入口。

### 2. ListChildren

负责列出某个目录下的直接子项。

返回的是 `DirectoryEntry` 视图，而不是完整 inode 集合，适合作为目录浏览接口。

### 3. ListFileChunks

负责按 chunk index 顺序返回一个文件的 chunk 列表。

这个接口是下载和校验类逻辑的基础。

### 4. BuildDownloadPlan

这是当前读路径里最重要的接口。

它会基于：

- `FileMetadata`
- `ChunkMetadata`
- `ReplicaSet`

组装出一个下载计划，返回：

- 文件大小、chunk 大小、状态
- 每个 chunk 的顺序、offset、size
- 每个 chunk 的候选节点集合
- 一个当前可优先使用的节点 `PreferredNodeID`

当前副本优先级规则是：

1. 优先健康副本
2. 同样健康时优先 `primary`
3. 再按节点 ID 稳定排序

这条规则比较简单，但足够支撑当前最小读流程。

---

## 当前测试覆盖了什么

`service_test.go` 现在已经覆盖：

- 创建目录的基本成功路径
- 创建文件的事务一致性
- 文件创建失败时 inode 回滚
- 启动上传会创建 session 并推进文件状态
- 并发 active upload session 会被拒绝
- 提交 chunk 会推进 session offset 和 stored size
- 完成上传会把 session、chunk 和 file 一起推进到 `verifying`
- 缺少已验证 file checksum 时，`VerifyUpload` 会失败
- chunk checksum 未验证时，`VerifyUpload` 会失败
- chunk 缺少最小可读副本时，`VerifyUpload` 会失败
- retryable 的校验失败会把 session 推进到 `retrying`
- `RetryUpload` 会从最近失败 offset 恢复上传
- 列目录会返回直接子项
- 下载计划会按 chunk 顺序返回，并优先选主副本
- rename / move 会同步维护 inode / file 路径
- 文件和目录删除会做级联清理

这说明当前文档里的主链路，不只是设计说明，而是已经有对应测试约束。

---

## 当前边界

虽然链路已经能走通，但现在还不是完整系统。

当前还没有覆盖的点包括：

- 异步 verifier 任务和后台 retry 调度
- 更复杂的副本健康判断和 file health 聚合
- 基于节点容量和拓扑的下载择优
- 真实数据节点读写

所以当前这两条链路的准确定位是：

“元数据层面的最小闭环，而不是完整分布式存储的数据面闭环。”
