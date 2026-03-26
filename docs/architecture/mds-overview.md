# MDS 总体设计

## 文档目标

这篇文档给 `AstraStorage` 的 Metadata Service(MDS) 提供一份总体设计总览。

它回答 5 个问题：

- MDS 在整个系统里负责什么
- 当前代码已经实现了什么
- 系统按什么模块拆分
- 核心数据对象之间是什么关系
- 后续实现应该优先往哪里补

更细的约束和实现细节分别放在这些文档里：

- [MDS Store 说明](./mds-store.md)
- [MDS 不变量清单](./mds-invariants.md)
- [MDS 内存 Store 说明](./mds-memory-store.md)
- [MDS Service 层说明](./mds-service.md)
- [MDS Handler 层说明](./mds-handler.md)
- [MDS RPC 层说明](./mds-rpc.md)
- [MDS 读写链路说明](./mds-flow.md)
- [MDS 启动与组装说明](./mds-bootstrap.md)
- [MDS Store 代码索引](./mds-store-code-map.md)
- [MDS 当前实现总览](./mds-implementation.md)

## MDS 的职责

MDS 负责管理分布式存储系统中的元数据，而不是直接保存文件内容。

它主要负责：

- 维护目录树和路径关系
- 维护文件元数据
- 维护文件切片、chunk 副本和节点分布
- 管理上传会话、断点续传和失败重试状态
- 为后续下载、校验、补副本和重平衡提供元数据基础

一句话概括：

MDS 管“文件应该是什么样、在哪里、现在进行到哪一步”，数据节点管“字节真正存在哪里”。

## 当前代码状态

当前仓库仍然处于“先把控制面和元数据链路做扎实”的阶段，但已经不只是设计稿。

已经比较完整的部分：

- `internal/mds/metadata/`
  定义 inode、file、chunk、replica、upload session、node 等核心模型
- `internal/mds/store/`
  定义 Repository 和事务接口，并提供内存版实现
- `internal/mds/`
  已经有最小 service、handler 和读写链路编排
- `internal/mds/rpc/`
  已经有进程内 request / response 类型和 router
- `cmd/mds/`
  已经能完成 `repo -> service -> handler -> router` 的启动组装
- `docs/architecture/`
  已有 store、service、handler、rpc、启动和读写链路说明

还主要是骨架或预留的部分：

- `internal/mds/placement/`
- `internal/mds/discovery/`
- `internal/mds/coordinator/`
- 真实网络协议层，例如 HTTP / gRPC server
- 持久化后端，例如 PostgreSQL / etcd

这意味着当前重点已经从“只有模型和仓储”推进到“最小业务闭环 + 进程内协议入口”，但还没有进入完整分布式运行阶段。

## 模块划分

### 1. metadata

目录：`internal/mds/metadata/`

负责定义核心领域对象：

- `inode`
- `file`
- `chunk`
- `replica`
- `upload session`
- `node`

这一层只定义“系统里有哪些数据”和“这些数据长什么样”，不负责存取实现。

### 2. store

目录：`internal/mds/store/`

负责定义元数据读写接口和事务边界。当前已经有内存版实现，主要用于：

- 本地开发
- 单元测试
- 提前验证不变量

这一层是当前仓库里最关键的可运行部分。

### 3. service / handler

目录：`internal/mds/`

当前已经承载最小业务用例，例如：

- 创建目录
- 创建文件
- 启动上传
- 提交 chunk
- 完成上传
- 校验上传并切换到 `available`
- 记录校验失败并恢复 retry
- rename / move inode
- 删除文件和递归删除目录
- 查询文件和目录元数据
- 列目录
- 列文件 chunk
- 生成下载计划

这一层已经通过事务把 inode、file、chunk、upload session 等对象串起来，并通过 `handler` 作为对上层的薄适配入口。

### 4. rpc

目录：`internal/mds/rpc/`

负责 request / response 类型和 method 路由。当前已经有进程内 router，但还没有接入真实网络协议层。

### 5. placement / discovery / coordinator

这三层是后续高级能力：

- `placement`
  决定 chunk 和副本应该放到哪些节点
- `discovery`
  管理节点注册、心跳和可用性视图
- `coordinator`
  处理补副本、迁移、重平衡和故障恢复

## 核心对象关系

MDS 的核心链路是：

`inode -> file -> chunk -> replica/node`

上传流程里还会增加：

`file -> upload session`

可以这样理解：

- `inode`
  管目录树结构，解决“文件在树上的哪里”
- `file`
  管文件实体，解决“这个文件本身是什么”
- `chunk`
  管文件切片，解决“这个文件被切成了几片”
- `replica`
  管 chunk 副本，解决“每片落在哪些节点上”
- `node`
  管存储节点状态，解决“哪些节点可用、容量是否足够”
- `upload session`
  管上传过程，解决“当前上传到了哪里”

这里最重要的一条边界是：

- `inode` 不负责分片内容
- `chunk` 不直接挂目录树
- `chunk` 通过 `FileID` 归属文件
- 文件再通过 `InodeID` 回到目录树

## 关键设计原则

### 1. 目录树和文件内容分离

`inode` 只关心树结构，不直接承载 chunk 信息。

这样做的好处是：

- rename / move 时只处理树关系和路径缓存
- 文件内容布局不会和目录结构耦合
- 后续做快照、版本或对象层扩展时更稳定

### 2. 路径是缓存，不是唯一真相

真正的树结构由 `ID + ParentID + Name` 决定。

`Path` 的作用是：

- 加快查询
- 便于展示
- 降低上层拼路径的成本

这意味着 rename 或 move 后，必须同步更新对应路径缓存。

### 3. chunk 是固定单位

当前设计里，chunk 固定单位为 `4 MiB`。

这条规则会影响：

- 上传切片
- 下载定位
- chunk offset 计算
- 最后一片之外的大小约束

### 4. 上传过程独立建模

上传状态放在 `UploadSession`，而不是混进 `FileMetadata`。

这样可以更清楚地区分：

- 文件最终状态
- 一次上传过程中的临时状态

对断点续传、重试、校验失败处理更友好。

### 5. 事务优先于单表操作

MDS 很多操作都不是改一个对象就结束，而是要同时改多层元数据。

典型例子：

- 创建文件时，同时创建 inode 和 file
- 上传过程中，同时写 chunk 和 upload session
- rename / move 时，同时更新 inode 和 file 路径
- 删除文件时，同时清理 file、chunk、upload session、replica

所以 `store` 层显式定义了事务接口。

## 典型流程

### 创建文件

1. 在目录树中创建文件型 inode
2. 创建对应的 `FileMetadata`
3. 初始化文件状态为 `pending` 或 `uploading`

### 上传文件

1. 创建 `UploadSession`
2. 客户端按 `4 MiB` 切片上传
3. 每个 chunk 写入后更新 chunk 元数据
4. 同步推进 upload session 的 offset
5. `CompleteUpload` 把 file / chunk / session 推进到 `verifying`
6. `VerifyUpload` 校验 checksum 和副本健康后切换到 `available`
7. 如果校验失败，`FailUploadVerification` 会把 file 推进到 `failed`，并把 session 推进到 `failed` 或 `retrying`
8. 如果允许重试，`RetryUpload` 会从最近失败 offset 重新打开上传窗口

### 下载文件

1. 根据路径或 inode 找到文件
2. 读取文件元数据和 chunk 列表
3. 根据 chunk 副本信息选择可读节点
4. 按 `Index` 或 `Offset` 顺序取回数据并重组文件

## 当前实现优先级

从当前仓库状态看，推荐按这个顺序继续推进：

1. 继续补异步 verifier、后台 retry 调度和 health 自动回写
2. 增加持久化后端实现
3. 为当前 router 接真实 HTTP / gRPC 暴露层
4. 最后补 placement、discovery、coordinator

原因很直接：

- 现在 store、service、router 已经有最小闭环，下一步更应该补状态机完整性
- 如果先上持久化和网络层，后续业务语义变更成本会更高

## 当前结论

这个仓库当前最重要的工作，不是继续加外层壳，而是继续把 MDS 核心元数据链路做扎实：

- 树结构要稳定
- 文件和切片关系要明确
- 上传状态要可恢复
- 副本和节点关系要能表达
- 不变量要能被测试锁住

这也是当前设计的主线。
