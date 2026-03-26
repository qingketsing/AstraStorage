# AstraStorage 全局设计

## 文档目标

这篇文档从项目全局视角说明 `AstraStorage` 的整体设计。

它主要回答这些问题：

- 这个项目整体要做什么
- 系统按哪些进程和模块拆分
- 每一层分别承担什么职责
- 元数据、数据面和基础设施之间怎么协作
- 当前代码已经落到了哪一层
- 后续应该按什么顺序继续实现

## 项目目标

`AstraStorage` 的目标是做一个面向 Kubernetes 场景的分布式云存储系统。

系统需要同时解决三类问题：

- 控制面：目录树、文件元数据、节点状态、放置策略、上传状态
- 数据面：文件切片的上传、存储、读取、复制和恢复
- 运维面：部署、监控、服务发现、健康检查和故障处理

当前仓库主要在建设控制面，尤其是 Metadata Service(MDS)。

## 全局模块划分

项目当前按 5 层来组织：

### 1. 进程入口层

目录：`cmd/`

每个子目录代表一个可执行程序：

- `cmd/mds/`
  元数据服务入口，当前是主线
- `cmd/gateway/`
  网关或统一接入层预留
- `cmd/monitor/`
  监控系统入口预留

这一层只负责进程启动、依赖装配和生命周期管理，不承载核心业务规则。

### 2. 业务核心层

目录：`internal/`

当前主要包含 4 个方向：

- `internal/mds/`
  元数据服务核心逻辑
- `internal/monitor/`
  监控系统相关业务逻辑预留
- `internal/shared/`
  多服务共享配置、事件和通用类型
- `internal/platform/`
  基础设施适配层

### 3. 元数据服务层

目录：`internal/mds/`

这是当前仓库最成熟的部分，负责：

- 目录树管理
- 文件元数据管理
- chunk 与副本元数据管理
- 上传会话和重试状态管理
- 节点状态与后续放置逻辑的元数据基础

它内部又拆成几块：

- `metadata/`
  领域模型
- `store/`
  存储接口和内存实现
- `service_*.go`
  业务编排与读写链路实现
- `handler.go`
  对上层的薄请求适配层
- `rpc/`
  进程内协议入口和传输结构
- `placement/`
  放置策略预留
- `discovery/`
  节点发现预留
- `coordinator/`
  协调控制预留

### 4. 平台适配层

目录：`internal/platform/`

这一层负责隔离基础设施依赖，避免业务层直接绑定具体中间件。

当前预留的基础设施方向包括：

- `postgres/`
  持久化、事务、迁移、健康检查
- `redis/`
  缓存、锁、Pub/Sub
- `mq/`
  消息投递和异步编排
- `kube/`
  Kubernetes 交互、服务发现、选主、控制器
- `observability/`
  日志、指标、链路追踪和健康检查

### 5. 文档与部署层

- `docs/`
  架构、部署和运行说明
- `deploy/`
  部署清单和环境资源
- `test/`
  测试资源、集成测试和端到端测试

## 逻辑架构

从逻辑上看，系统可以拆成 4 个大块：

### 1. 元数据控制面

核心是 MDS。

它保存和提供：

- inode 目录树
- 文件元数据
- chunk 元数据
- replica 元数据
- upload session
- node 状态

这一层是整个系统的“事实来源”之一，负责回答：

- 文件在哪个目录下
- 文件有哪些 chunk
- 每个 chunk 在哪些节点上
- 上传是否完成
- 哪些节点当前健康

### 2. 数据存储面

这部分当前还没有正式实现代码，但从模型上已经被设计出来。

它的职责会包括：

- 接收 chunk 写入
- 保存 chunk 副本
- 提供按 chunk 读取能力
- 向 MDS 回报写入状态、校验结果和心跳

MDS 不直接保存数据字节，真正的数据面节点会保存 chunk 内容。

### 3. 接入与协议层

这部分未来大概率会由 `gateway` 和 `mds/rpc` 共同承担。

它负责：

- 接收客户端请求
- 做鉴权、路由、限流和协议适配
- 转发到 MDS 或数据节点
- 返回统一错误模型和响应结构

### 4. 运维与控制层

这部分由 monitor、discovery、coordinator、observability 共同组成。

它负责：

- 节点发现
- 节点健康和容量感知
- 副本补齐
- 故障恢复
- 重平衡
- 指标、日志和告警

## 核心数据关系

当前项目的核心元数据链路是：

`inode -> file -> chunk -> replica -> node`

上传过程会额外引入：

`file -> upload session`

可以这样理解：

- `inode`
  管目录树结构
- `file`
  管文件实体
- `chunk`
  管文件切片
- `replica`
  管每片的副本分布
- `node`
  管节点状态
- `upload session`
  管一次上传过程

这里有一个明确的设计原则：

- 树结构和内容布局分离
- 元数据和真实字节数据分离
- 稳定对象状态和临时上传状态分离

这使得后续 rename、move、续传、补副本、迁移都可以各自演进，而不是耦在一个大对象里。

## 关键数据流

### 文件创建

1. 请求进入 MDS
2. 创建文件型 inode
3. 创建 file metadata
4. 返回文件标识和后续上传上下文

### 文件上传

1. 创建 upload session
2. 客户端按 `4 MiB` 切片
3. 由 placement 决定 chunk 应落到哪些节点
4. 数据节点保存 chunk
5. MDS 更新 chunk、replica、upload session、file 状态
6. 上传完成后进入校验和可读状态

### 文件下载

1. 根据路径或文件 ID 查 MDS
2. 获取 file 与 chunk 列表
3. 为每个 chunk 选择可读副本节点
4. 从数据节点取回 chunk
5. 按顺序重组文件

### 故障恢复

1. discovery / 心跳层发现节点异常
2. coordinator 分析受影响的 replica
3. placement 重新选择目标节点
4. 系统触发补副本或迁移
5. MDS 更新 chunk 与 file 的分布状态

## 当前代码落点

当前仓库还没有形成完整分布式系统，但已经有一条最小可运行的 MDS 进程内链路。当前主要落在下面几层：

- MDS 领域模型
- store 接口
- 内存版 store 实现
- service 业务编排
- handler 薄适配层
- 进程内 RPC router
- `cmd/mds` 启动组装
- 架构文档和不变量文档

还没有正式落地的关键部分包括：

- 持久化后端实现
- 真实网络协议层实现
- placement / discovery / coordinator 逻辑
- 数据面节点和真实 chunk 读写

换句话说，当前代码已经从“只有模型和仓储”推进到“有最小业务闭环与进程入口”，但仍然以控制面为主。

## 当前阶段定位

整体上看，这个项目目前更接近：

“一个以 MDS 为中心、已经实现最小读写链路和进程内 RPC 的分布式存储控制面原型”。

接下来最值得补的部分是：

- 数据节点实现
- gateway 或真实对外接入层
- monitor 实现
- 持久化后端
- placement / discovery / coordinator

## 当前最重要的设计判断

### 1. 先稳控制面，再补数据面

现在已经有了最小 MDS 读写链路，所以接下来更重要的是补“完整性”和“可运行性”，而不是过早扩展外围模块。

### 2. 先让当前进程内链路对外可用，再继续加高级调度

在没有真实网络暴露层和持久化后端之前，placement、discovery、coordinator 还缺少稳定运行基础。

### 3. 平台适配应该接在清晰的接口之后

像 PostgreSQL、Redis、MQ、Kubernetes 这些依赖，仍然应该建立在已经稳定的业务接口和协议边界之上。

## 建议的实现路线

推荐按这个顺序推进：

1. 继续补删除、rename、move 的跨表一致性
2. 把当前 `mds/rpc` 接到真实 HTTP / gRPC
3. 落地 PostgreSQL 仓储实现
4. 引入 discovery、placement、coordinator
5. 再扩展 gateway、monitor 和更完整的数据面

## 相关文档

- [项目结构说明](../../PROJECT_STRUCTURE.md)
- [MDS 当前实现总览](./mds-implementation.md)
- [MDS 总体设计](./mds-overview.md)
- [MDS Store 说明](./mds-store.md)
- [MDS 内存 Store 说明](./mds-memory-store.md)
- [MDS Service 层说明](./mds-service.md)
- [MDS RPC 层说明](./mds-rpc.md)
