# MDS 当前实现总览

## 这份文档是干什么的

这份文档说明 `AstraStorage` 里 MDS 当前已经落地了哪些实现，以及这些实现分别应该去哪篇文档里看。

它的定位不是替代各模块说明，而是作为“当前实现地图”。

如果你想快速知道：

- 现在已经做到了哪一步
- 哪些部分已经可运行
- 每一层各自负责什么
- 当前读写链路已经打通到哪里

先看这篇最合适。

---

## 推荐阅读顺序

如果你想系统性理解当前实现，建议按下面顺序读：

1. [system-overview.md](./system-overview.md)
   先建立整个项目的全局图。
2. [mds-overview.md](./mds-overview.md)
   再聚焦 MDS 在系统里的职责和模块拆分。
3. [mds-implementation.md](./mds-implementation.md)
   明确当前已经实现到了哪一层。
4. [mds-store.md](./mds-store.md)
   了解元数据抽象和事务边界。
5. [mds-memory-store.md](./mds-memory-store.md)
   了解当前可运行的底座实现。
6. [mds-invariants.md](./mds-invariants.md)
   对照不变量理解为什么要这样建模。
7. [mds-service.md](./mds-service.md)
   再看业务编排层。
8. [mds-flow.md](./mds-flow.md)
   把当前读写链路串起来。
9. [mds-handler.md](./mds-handler.md)
   看请求适配层。
10. [mds-rpc.md](./mds-rpc.md)
    看进程内协议入口和 router。
11. [mds-bootstrap.md](./mds-bootstrap.md)
    最后看 `cmd/mds` 如何把各层接起来。

如果你只想快速进入当前主线，最短路径是：

1. [mds-implementation.md](./mds-implementation.md)
2. [mds-memory-store.md](./mds-memory-store.md)
3. [mds-service.md](./mds-service.md)
4. [mds-flow.md](./mds-flow.md)
5. [mds-rpc.md](./mds-rpc.md)

---

## 当前已经实现到什么程度

目前 MDS 已经不只是设计骨架，而是有了一条最小可运行链路：

1. `store`
   已有内存版实现和事务支持
2. `service`
   已有目录、文件、上传、读路径和下载规划的业务编排
3. `handler`
   已有薄转发层
4. `rpc`
   已有进程内 request / response 类型和 router
5. `cmd/mds`
   已能完成 `repo -> service -> handler -> router` 的启动组装

也就是说，当前仓库已经可以在进程内完成：

- 创建目录
- 创建文件
- 启动上传
- 提交 chunk
- 完成上传
- 校验上传并切换到 `available`
- 记录校验失败并恢复 retry
- rename / move inode
- 删除文件和递归删除目录
- 查询 inode / file / upload session
- 列目录
- 列文件 chunk
- 生成下载计划

---

## 当前已经打通的链路

### 写路径

当前最完整的写路径是：

`CreateDirectory -> CreateFile -> StartUpload -> CommitChunk -> CompleteUpload -> VerifyUpload`

在校验失败分支上，当前也已经打通：

`CompleteUpload -> FailUploadVerification -> RetryUpload -> CommitChunk`

这条链路已经贯通：

- `service`
- `handler`
- `rpc router`
- `cmd/mds` 组装入口

### 读路径

当前最完整的读路径是：

- `GetInode`
- `GetFile`
- `GetUploadSession`
- `ListChildren`
- `ListFileChunks`
- `BuildDownloadPlan`

其中 `BuildDownloadPlan` 已经能基于 chunk 和 replica 信息生成顺序下载计划。

---

## 还没有实现的部分

虽然当前已经有一条最小闭环，但还没进入完整系统阶段。

现在仍然缺少的主要部分包括：

- 真实网络协议层，例如 HTTP / gRPC server
- 持久化后端，例如 PostgreSQL / etcd
- placement 调度策略
- discovery / heartbeat 体系
- coordinator、故障恢复和重平衡
- 异步 verifier 和后台 retry 调度
- 副本健康、文件健康与补副本的自动闭环
- 更完整的错误模型和权限控制

所以当前状态更准确地说是：

“MDS 的核心元数据模型、存储抽象、最小业务链路和进程内 RPC 已经落地，但还没有进入完整分布式运行阶段。”
