# MDS Service 层说明

## 这份文档是干什么的

这份文档说明当前仓库里刚开始落地的 `mds` service 层：

- 它为什么存在
- 它和 `store` 层的边界是什么
- 它现在已经实现了哪些业务动作
- 它后面应该如何继续扩展

对应代码在：

- [service.go](../../internal/mds/service.go)
- [service_directory.go](../../internal/mds/service_directory.go)
- [service_file.go](../../internal/mds/service_file.go)
- [service_upload.go](../../internal/mds/service_upload.go)
- [service_mutation.go](../../internal/mds/service_mutation.go)
- [service_helpers.go](../../internal/mds/service_helpers.go)
- [service_test.go](../../internal/mds/service_test.go)

---

## 为什么要有 service 层

`store` 只负责“怎么保存对象”和“单个仓储操作怎么保证约束”。

但真正的业务动作通常不是改一张逻辑表就结束，例如：

- 创建文件时，要同时创建 `inode` 和 `file`
- 启动上传时，要同时创建 `upload session` 并更新文件状态

这些跨对象操作不适合直接散落在 handler 或 RPC 层里，所以需要一层显式的 service 做编排。

这层的定位是：

- 组织事务
- 串联多个 repository 操作
- 承接后续 HTTP / gRPC / CLI 调用

---

## 当前整体结构

当前 service 层仍然放在 `internal/mds/` 下，并按职责拆成多个文件：

- `service.go`
  定义 `Service` 本体和依赖注入入口
- `service_directory.go`
  处理目录相关用例
- `service_file.go`
  处理文件创建和查询
- `service_upload.go`
  处理上传初始化
- `service_helpers.go`
  放路径、时间、clone 等辅助函数

现在还没有再下沉到单独 package，原因是当前规模不大，继续保持同包多文件更符合 Go 的简单组织方式。

---

## 当前已经实现的能力

### CreateDirectory

`CreateDirectory` 会：

1. 校验请求参数
2. 读取父节点
3. 确认父节点是目录
4. 组装新目录的 `InodeMetadata`
5. 在事务中调用 `CreateInode`

### CreateFile

`CreateFile` 会：

1. 校验请求参数
2. 读取父目录 inode
3. 构造文件型 inode
4. 构造 `FileMetadata`
5. 在同一个事务里依次创建 inode 和 file

这一步的重点是：如果 `CreateFile` 阶段失败，inode 也会一起回滚。

### StartUpload

`StartUpload` 会：

1. 校验请求参数
2. 读取目标文件
3. 检查文件状态是否允许启动上传
4. 创建 `UploadSession`
5. 在同一个事务里把文件状态推进到 `uploading`
6. 回写 `LatestUploadSessionID`

并且当前已经会拒绝同一文件同时存在多个未终结 upload session。

### CommitChunk / CompleteUpload / VerifyUpload / FailUploadVerification / RetryUpload

`CommitChunk` 现在会：

1. 校验 upload session 仍然可写
2. 写入或覆盖 chunk 元数据
3. 推进 upload session 的 offset
4. 回写 `LastPersistedChunk`
5. 重新计算 `StoredSize`

`CompleteUpload` 现在会：

1. 校验 chunk 覆盖是否完整
2. 把 upload session 推进到 `verifying`
3. 把 chunk 推进到 `verifying`
4. 把文件推进到 `verifying`

`VerifyUpload` 现在会：

1. 校验每个 chunk 是否具备最小可读副本
2. 校验每个 chunk 的 checksum 已验证
3. 校验文件级最终 checksum 已验证
4. 把 upload session 切到 `completed`
5. 把所有 chunk 推进到 `available`
6. 把文件推进到 `available`

`FailUploadVerification` 现在会：

1. 记录一次校验失败上下文
2. 把 upload session 推进到 `failed` 或 `retrying`
3. 把当前文件推进到 `failed`
4. 把当前 chunk 集合推进到 `failed`
5. 回写失败 offset、失败 chunk 和重试元数据

`RetryUpload` 现在会：

1. 只接受 `retrying` 状态的 upload session
2. 按最近一次失败 offset 重建可继续写入的上传窗口
3. 保留失败点之前的已持久化 chunk
4. 删除失败点及之后的 chunk
5. 清空旧的 verified checksum
6. 把 upload session 和 file 重新推进到 `active / uploading`

### Rename / Move / Delete

当前 service 层已经补上高风险一致性流程：

- `RenameInode`
  文件 rename 时会同步更新 inode 和 file 的 `Name`、`Path`
- `MoveInode`
  文件 move 时同步更新 inode 和 file；目录 move / rename 时会更新整棵子树 inode path，并同步更新子树下所有 file path
- `DeleteFile`
  会在同一个事务里清理 file、inode、chunk 和 upload session
- `DeleteDirectory`
  支持显式递归删除；递归过程中会级联删除目录下文件及其关联元数据

---

## 它和 store 层的关系

可以这样理解：

- `store`
  负责单个对象怎么存、怎么查、怎么做基础约束校验
- `service`
  负责一个业务动作需要调用哪些 store 接口，以及事务边界在哪里

所以 service 层不应该重新实现底层持久化细节，而应该专注于：

- 请求校验
- 对象组装
- 事务编排
- 跨表一致性

---

## 当前测试覆盖了什么

目前 `service_test.go` 已经覆盖了这些最小场景：

- 创建目录后路径正确
- 创建文件时 inode 和 file 会一起落库
- file 创建失败时，事务会把 inode 一起回滚
- 启动上传时，会创建 session 并更新文件状态
- 同一文件的并发 active upload session 会被拒绝
- `CompleteUpload` 会把 file/session/chunk 推进到 `verifying`
- `VerifyUpload` 前必须满足 checksum 和最小可读副本约束
- retryable 的校验失败会把 session 推进到 `retrying`
- `RetryUpload` 会按失败 offset 清理 chunk 并恢复上传
- rename / move 会维护 inode 与 file 路径一致性
- 删除会在事务里做级联清理

这些测试的重点不是单表 CRUD，而是“service 有没有正确组织事务”。

---

## 当前边界

这层现在已经不只是最小业务编排层，目录变更、删除和显式 verifying 状态机也已经落地。

当前还没实现的重点包括：

- 异步 verifier 和后台重试调度
- replica 与 file 健康度的自动回写
- 更复杂的失败恢复、补副本和重平衡编排
- 更完整的错误模型和外部协议映射

所以这层当前的实际价值是：

- 给后续 handler / RPC 提供稳定调用入口
- 把最小上传链路先从“仓储能力”提升到“业务动作”
- 为后续继续补 service API 留出清晰结构
