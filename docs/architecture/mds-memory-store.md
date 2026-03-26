# MDS 内存版 Store 说明

## 这份文档是干什么的

这份文档说明当前仓库里刚实现的内存版 `store`：

- 它解决了什么问题
- 它现在支持哪些能力
- 它是怎么处理事务和数据隔离的
- 它目前还没覆盖哪些约束

对应代码在：

- [memory.go](../../internal/mds/store/memory.go)
- [memory_test.go](../../internal/mds/store/memory_test.go)

---

## 为什么先做内存版

当前项目的 `store` 接口已经定义好了，但还没有任何后端实现。

如果没有一个最小可用实现，后续这些层都没法真正往前推进：

- `server`
- `handler`
- 上传流程
- 事务边界测试

所以内存版的定位不是“最终存储后端”，而是：

- 给 service 层提供一个可调用的仓储实现
- 把关键不变量先变成可执行代码
- 给单元测试提供稳定、低成本的依赖

---

## 整体结构

内存版仓储由一个 `memoryRepository` 和一个 `memoryState` 组成。

`memoryState` 里有 5 组核心数据：

- `inodes`
- `files`
- `chunks`
- `uploadSessions`
- `nodes`

每一类数据都用内存 map 保存，仓储外层通过 `sync.RWMutex` 做并发保护。

读操作直接查当前状态。
写操作先做校验，再修改当前状态。

---

## 事务模型

事务采用“快照复制”方式实现：

1. `BeginTx` 时复制当前 `memoryState`
2. 事务内所有修改都落到副本上
3. `Commit` 时把副本整体替换回主仓储
4. `Rollback` 时直接丢弃副本

这种方式的优点是：

- 实现简单
- 测试行为稳定
- 很容易表达“事务内原子更新”

当前这套事务语义适合本地开发和单元测试，但它不是正式数据库事务，不做并发冲突检测。

---

## 当前已实现的关键约束

### inode

- 根目录只能创建一次
- 根目录不能有父节点
- 非根节点必须有已存在的父目录
- 父节点必须是目录
- 同一父目录下不能重名
- 目录类型 inode 会清空 `FileID`
- `deleting` / `deleted` 节点不能 rename 或 move
- 目录不能被移动到自己的子树下面

### file

- `FileID` 和 `InodeID` 必填
- 绑定的 inode 必须存在
- 绑定的 inode 必须是文件类型
- `ChunkSize` 默认并强制为 `4 MiB`

### chunk

- `ChunkID`、`FileID` 必填
- chunk 大小不能为负
- chunk 大小不能超过 `4 MiB`
- `offset` 必须等于 `index * 4 MiB`

### upload session

- `SessionID`、`FileID` 必填
- file 必须先存在
- `ChunkSize` 固定为 `4 MiB`
- 已完成会话不能继续更新进度
- `ConfirmedOffset` 不能大于 `NextOffset`
- 校验失败会把 session 推进到 `failed` 或 `retrying`
- retry 恢复时支持清空旧的 verified checksum

### node

- `NodeID` 必填
- `Capacity` 和 `Used` 不能为负

---

## 为什么有很多 clone 函数

仓储在返回对象前会复制数据，而不是直接把内部 map 里的对象指针暴露出去。

这样做是为了避免调用方绕过仓储接口，直接修改内存里的真实状态，导致不变量失效。

这也是事务复制模型能保持行为稳定的前提之一。

---

## 当前测试覆盖了什么

目前已经有一批最小测试：

- 根目录重复创建会失败
- 同目录重名会失败
- 目录 rename 后，子树路径可以批量更新
- `InTx` 中返回错误时，事务会回滚

这些测试是第一批护栏，后面应该继续补：

- `CreateFile` 和 inode 类型约束
- 上传会话 offset 边界
- 完成态 session 不可继续写入
- inode / file 路径一致性

---

## 当前边界

这版内存实现已经足够支撑 service 层继续开发，但还不是完整语义实现。

目前还没有覆盖的重点包括：

- inode 和 file 的跨表一致性校验
- 更接近真实数据库的并发事务冲突检测
- 更严格的 chunk 编号唯一性约束
- 更完整的持久化索引、外键和约束验证

所以它现在更适合作为“开发和测试底座”，而不是最终持久化方案。
