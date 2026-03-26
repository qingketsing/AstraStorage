# MDS Store 代码索引

这个文档用于说明 `internal/mds/store/` 目录下各个文件的职责，以及推荐的阅读顺序。

如果你第一次阅读这一层代码，建议按下面顺序看：

1. `internal/mds/store/store.go`
   先看仓储接口定义，理解 `inode`、`file`、`chunk`、`upload session`、`node` 这些能力边界。

2. `internal/mds/store/txn.go`
   再看事务接口，理解哪些操作预期要放进同一个原子边界里。

3. `internal/mds/store/memory.go`
   这里是内存版实现的入口，只保留共享错误、仓储结构和 `NewMemoryRepository()`。

4. `internal/mds/store/memory_tx.go`
   说明内存事务怎么做。当前实现采用“快照复制 + 整体提交”的方式。

5. `internal/mds/store/memory_inode.go`
   目录树相关逻辑，包含根目录唯一、父目录校验、重名检查、rename / move / subtree path 更新。

6. `internal/mds/store/memory_file.go`
   文件元数据相关逻辑，主要处理 file 与 inode 的绑定、固定 chunk size、放置信息更新。

7. `internal/mds/store/memory_chunk.go`
   chunk 相关逻辑，主要校验 chunk size、offset/index 关系，以及 chunk 副本集合更新。

8. `internal/mds/store/memory_upload.go`
   上传会话相关逻辑，处理 session 创建、进度更新、失败记录、完成状态切换。

9. `internal/mds/store/memory_node.go`
   节点相关逻辑，处理节点 upsert、过滤查询和心跳更新。

10. `internal/mds/store/memory_helpers.go`
    放公共工具：深拷贝、selector 查询、路径处理、分页窗口等。

## 设计约定

- 所有 `memory_*.go` 文件都属于同一个 `store` package，不额外拆子 package。
- 当前内存版实现的目标是提供“可运行、可测试的最小后端”，不是最终持久化方案。
- 返回对象时会做深拷贝，避免调用方直接修改仓储内部状态。
- 事务只适合本地开发和测试，不提供正式数据库级别的并发冲突处理。

## 相关文件

- `internal/mds/store/memory_test.go`
  当前内存版实现的单元测试。

- `docs/architecture/mds-store.md`
  这一层的接口设计说明。

- `docs/architecture/mds-memory-store.md`
  当前内存版实现的设计说明和边界说明。
