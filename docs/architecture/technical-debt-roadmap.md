# AstraStorage 增强项与技术债路线图

## 文档目标

这份文档用于集中记录当前仓库里那些“为了先打通链路而选择了简单实现”的部分。

它不是问题清单，也不是缺陷列表，而是一个持续更新的增强路线图，主要回答四个问题：

- 当前为什么这样做
- 这样做的风险是什么
- 这部分技术债应该在什么时候偿还
- 整体应该按什么顺序偿还

这份文档建议在下面几种情况下同步更新：

- 新增了一个明显的 MVP / 原型级实现
- 某个模块从“最小闭环”进入“增强版”
- 某项技术债已经偿还，可以从列表中移除或降级
- 项目的阶段目标发生了变化

---

## 当前判断标准

这里记录的“需要增强”并不等于“现在就是错的”。

很多实现是当前阶段的合理选择，因为它们：

- 能更快验证系统主链路
- 能把错误范围控制在更小的模块内
- 能为后续优化保留清晰边界

但这些实现一旦进入更真实的场景，就会暴露出吞吐、扩展性、恢复能力或可维护性上的问题，所以需要提前记账。

---

## 还债顺序总览

建议按下面顺序偿还：

1. `gateway` 多副本上传、并发写入与流式传输
2. `gateway` 删除闭环与 orphan chunk 清理
3. `datanode` 真实容量统计、落盘管理和节点状态上报
4. `MDS` 真正的 placement / allocator 能力
5. `coordinator / discovery` 的后台恢复与调度闭环
6. `PostgreSQL` 与 `memory store` 中为开发便利保留的简化语义
7. 服务化配套：鉴权、限流、审计、观测、运维接口

这个顺序的核心原则是：

- 先补数据主链路缺口
- 再补一致性和恢复能力
- 最后补吞吐、平台化和生产化能力

---

## 增强项清单

### 1. Gateway 上传与副本修复已经成型，但整体仍然是串行与轮询语义

相关位置：

- [http.go](/home/qingke/AstraStorage/internal/gateway/http.go)
- [client.go](/home/qingke/AstraStorage/internal/gateway/client.go)
- [http.go](/home/qingke/AstraStorage/internal/datanode/http.go)
- [repairer.go](/home/qingke/AstraStorage/internal/mds/coordinator/repairer.go)

当前为什么这样做：

- 先让上传路径和副本修复路径共用同一条 datanode 内部复制 RPC
- 把复杂度控制在“gateway 串行切片 + primary 节点转发 + MDS 后台轮询修复”的模型里
- 当前 repair loop 已经补了基础退避和单轮修复上限，让它能长期运行而不是高频重打同一副本
- 让 `pending` 副本能够被后续自动补齐，而不是长期停留在元数据里

风险是什么：

- 上传吞吐明显受限，网关会成为串行瓶颈
- 后台修复目前仍然是轮询扫描，不是事件驱动
- 主节点转发和后台修复都还是 best-effort，没有更强的复制流水线控制
- 一旦主节点本地成功但部分副本失败，系统虽然会补齐，但仍缺少持久化任务状态、更细的幂等控制和优先级调度
- 后续引入并发上传和更真实的复制流水线时，当前逻辑仍需要进一步演进

什么时候该还：

- 在准备支持更高吞吐、多节点压力和更复杂失败场景之前
- 在准备测试更大文件、更高吞吐或多节点环境之前

还债顺序：

- 顺序 `1`

建议增强方向：

- 先把后台修复从轮询扫描演进到事件驱动队列
- 再演进到并发 `PutChunk` + 顺序 `CommitChunk`
- 最后补失败回滚、孤儿 chunk 清理和幂等重试

### 2. Gateway 上传/下载仍然把完整文件放进网关内存

相关位置：

- [http.go](/home/qingke/AstraStorage/internal/gateway/http.go)

当前为什么这样做：

- 最小实现最直接
- 测试容易写
- 能快速证明字节链路闭环已经存在

风险是什么：

- 大文件会直接放大网关内存压力
- 无法形成真正的流式上传/下载
- 在客户端并发增大时，网关会成为明显瓶颈

什么时候该还：

- 在支持多 chunk 上传之后立即跟进
- 在准备测试更大文件或并发流量之前

还债顺序：

- 顺序 `2`

建议增强方向：

- 上传改成流式切片
- 下载改成边拉边写，而不是先聚合再返回

### 3. Gateway 已经具备文件删除闭环，但仍缺目录级数据面删除和失败清理

相关位置：

- [http.go](/home/qingke/AstraStorage/internal/gateway/http.go)
- [client.go](/home/qingke/AstraStorage/internal/gateway/client.go)

当前为什么这样做：

- 先把最常见的文件删除场景闭环做通
- 让 `gateway` 能在删除元数据前，先按 chunk 副本把 datanode 上的真实数据清掉
- 避免一开始就把目录递归删除、失败补偿和 orphan 清理一起做大

风险是什么：

- 目录递归删除仍然只有 MDS 元数据级联，没有数据面联动清理
- 上传中途失败时可能残留 orphan chunk
- 删除过程中如果 datanode 清理与 MDS 删除之间发生故障，仍需要重试和补偿
- 系统长期运行后，数据面和元数据面仍可能逐渐漂移

什么时候该还：

- 在准备开放目录删除或长期运行测试之前
- 在准备做长期运行测试之前

还债顺序：

- 顺序 `3`

建议增强方向：

- 给目录递归删除补上数据面清理
- 上传失败时记录并清理 orphan chunk
- 给删除链路补重试、补偿和审计日志

### 4. Datanode 还是单机本地文件系统原型

相关位置：

- [store.go](/home/qingke/AstraStorage/internal/datanode/store.go)

当前为什么这样做：

- 文件系统最容易验证“真实数据确实落盘”
- 不引入额外存储引擎，便于先把上层链路打通

风险是什么：

- 元数据 sidecar + 二进制文件的模型不适合高吞吐
- 缺少更强的落盘组织、压缩、碎片控制和后台清理
- 对多盘、多卷、多层存储没有抽象

什么时候该还：

- 在开始测更真实的数据量和吞吐之前
- 在 datanode 不再只是本地开发原型时

还债顺序：

- 顺序 `4`

建议增强方向：

- 引入更明确的 chunk 目录布局和容量管理
- 逐步抽象存储后端，而不是直接绑死本地目录

### 5. Datanode 已经上报真实 `used bytes`，但容量模型仍然是最小实现

相关位置：

- [app.go](/home/qingke/AstraStorage/cmd/datanode/app.go)
- [config.go](/home/qingke/AstraStorage/internal/datanode/config.go)

当前为什么这样做：

- 当前已经从 datanode 的 `chunks/` 落盘目录统计真实字节占用，并通过注册/心跳把 `used` 回写到 MDS
- 这样做先解决“容量感知调度没有可信输入”的硬问题
- 但仍然保持本地文件系统原型，不把容量模型一下子做成真正的多盘/多层存储抽象

风险是什么：

- 当前 `used` 只反映 chunk 目录里的实际文件大小
- 还没有把文件系统保留空间、后台临时文件、磁盘水位线和多盘布局纳入模型
- 如果后续做更严格的调度或容量保护，仍需要更精细的空间语义

什么时候该还：

- 在准备支持更真实的磁盘水位保护、多盘节点或更严格的容量阈值之前

还债顺序：

- 顺序 `5`

建议增强方向：

- 在真实字节占用的基础上，再引入更严格的空间阈值
- 区分用户数据、元数据 sidecar、临时文件和预留空间
- 为多盘或多卷 datanode 预留容量抽象

### 6. MDS 已有最小容量感知 allocator，但离完整 placement 还很远

相关位置：

- [service_node.go](/home/qingke/AstraStorage/internal/mds/service_node.go)
- [allocator.go](/home/qingke/AstraStorage/internal/mds/allocator.go)

当前为什么这样做：

- 先让 gateway 不再硬编码 datanode 地址
- 先把“分配”这个控制面动作真正落下来
- 这次已经把 upload target allocation 和 repair target filtering 收到同一套最小容量规则里
- 当前策略故意只做到“健康 + 地址可用 + 可用容量大于 0 + 稳定排序”

风险是什么：

- 没有 zone / rack / region 故障域约束
- 没有负载均衡和热点控制
- 还没有 chunk-size-aware 的最小剩余空间保护
- 多副本场景下仍然可能做出次优放置决策

什么时候该还：

- 在支持真正多副本写入之前
- 在准备跑多节点联调环境之前

还债顺序：

- 顺序 `6`

建议增强方向：

- 继续沿 allocator 边界演进，而不是把策略散回 service 和 repairer
- 先补 chunk-size-aware 最小剩余空间
- 再逐步引入容量、健康度、故障域和打分模型

### 7. Placement / Discovery / Coordinator 已经形成第一版调度闭环，但仍然不是最终调度系统

相关位置：

- [allocator.go](/home/qingke/AstraStorage/internal/mds/allocator.go)
- [app.go](/home/qingke/AstraStorage/cmd/mds/app.go)
- [elector.go](/home/qingke/AstraStorage/internal/platform/etcd/leader/elector.go)
- [supervisor.go](/home/qingke/AstraStorage/internal/mds/coordinator/supervisor.go)
- [system-overview.md](/home/qingke/AstraStorage/docs/architecture/system-overview.md)

当前为什么这样做：

- 这一轮已经把 `failover`、`cleanup`、`rebalance` 和现有 `repairer` 收成了第一版完整调度闭环
- `ReplicaPlan` 现在会持久化到 PG，planner 负责生成计划，`repairer` 负责复制，`cleanup` 负责收尾
- 多 `MDS` 实例场景下，所有 controller loops 都已挂到同一套 etcd-backed leader supervisor 下
- 当前仍然刻意保持 scan-based controller，而不是一步走到任务引擎或事件驱动架构

风险是什么：

- 调度闭环虽然已经形成，但当前 planner 仍然依赖全量扫描，不是 watch 或事件驱动
- `ReplicaPlan` 已经持久化，但当前还没有更细的 task ownership / claim / distributed fencing 语义
- rebalance 目前只做到最小容量感知迁移，没有故障域、热点和更复杂的打分模型
- cleanup 已经能做最小收尾，但还没有把 orphan upload chunk 和更广泛的数据面清理一起纳入

什么时候该还：

- 在准备把系统描述成“更完整的调度器”之前
- 在多节点长期运行和更复杂故障注入之前

还债顺序：

- 顺序 `7`

建议增强方向：

- 先把 `ReplicaPlan` 的状态流转和 ownership 继续收紧
- 再把调度从 scan-based planner 演进到 watch 或事件驱动
- 然后引入更完整的 placement policy：故障域、热点控制、打分模型
- 最后再评估是否需要演进成通用任务引擎

### 8. PostgreSQL 仓储会自动补 placeholder node

相关位置：

- [node.go](/home/qingke/AstraStorage/internal/platform/postgres/repository/node.go)
- [file.go](/home/qingke/AstraStorage/internal/platform/postgres/repository/file.go)

当前为什么这样做：

- 为了让上传主链路在真实数据库上先跑通
- 避免 service 当前还没显式注册节点时被外键直接卡死

风险是什么：

- 数据库里会出现“只有 node id，没有完整地址和属性”的节点记录
- 节点注册和副本写入的语义边界会被冲淡

什么时候该还：

- 在 datanode 注册链路稳定后
- 在准备把 node 记录作为真正调度依据之前

还债顺序：

- 顺序 `8`

建议增强方向：

- 改成“未注册节点不能被引用”
- 或至少区分 placeholder node 和 registered node

### 9. Memory Store 明确是简单事务模型，不适合高并发

相关位置：

- [memory_tx.go](/home/qingke/AstraStorage/internal/mds/store/memory_tx.go)

当前为什么这样做：

- 深拷贝快照事务最容易保证单测稳定
- 适合本地开发和纯业务测试

风险是什么：

- 并发冲突检测能力有限
- 不适合性能测试或更真实的并发语义验证

什么时候该还：

- 不需要优先偿还
- 只有在确实想把 memory backend 用作更强的并发仿真环境时才需要增强

还债顺序：

- 顺序 `9`

建议增强方向：

- 保持其测试用途定位，不建议优先投入太多工程量

### 10. 一些核心规则仍然是“写死约束”

相关位置：

- [memory_file.go](/home/qingke/AstraStorage/internal/mds/store/memory_file.go)
- [service_upload.go](/home/qingke/AstraStorage/internal/mds/service_upload.go)
- [mds-store.md](/home/qingke/AstraStorage/docs/architecture/mds-store.md)

当前为什么这样做：

- 固定 `4 MiB` chunk size
- 固定最小副本语义
- 固定一些状态推进条件

这些约束有利于先稳定系统主线。

风险是什么：

- 后续支持不同对象策略时会受限
- 更复杂的工作负载可能需要不同 chunk 策略

什么时候该还：

- 在系统需要支持多种 workload profile 之前
- 在 chunk 策略开始影响吞吐和成本时

还债顺序：

- 顺序 `10`

建议增强方向：

- 先把策略参数化
- 再决定是否做多租户或多对象类型差异配置

### 11. 服务化配套仍未进入生产级，但应用层 observability foundation 已经落地

相关位置：

- [handler.go](/home/qingke/AstraStorage/internal/mds/handler.go)
- [http.go](/home/qingke/AstraStorage/internal/mds/rpc/http.go)
- [grpc.go](/home/qingke/AstraStorage/internal/mds/rpc/grpc.go)

当前为什么这样做：

- 先把协议和业务动作稳定下来
- 不把鉴权、限流、审计、追踪一起混进主链路开发
- 当前已经补齐第一阶段 observability foundation：
  - 三个服务统一 `/metrics`
  - 统一 JSON 结构化日志
  - `X-Request-ID` 透传
  - `gateway` / `mds` / `datanode` 的核心业务指标

风险是什么：

- 不能作为生产对外服务直接暴露
- 问题排查能力已经明显提升，但仍缺 tracing、dashboard 和 alerting
- 安全控制能力仍然有限
- 还没有 Redis / RabbitMQ / PostgreSQL / Kubernetes 的基础设施观测层

什么时候该还：

- 在准备多用户或跨服务接入之前
- 在准备上线预生产环境之前

还债顺序：

- 顺序 `11`

建议增强方向：

- 健康检查、metrics、结构化日志、request id 已完成
- 下一步补 tracing、dashboard、告警规则
- 再补鉴权、限流和审计

### 12. 当前 observability 仍然是应用层，不是完整监控栈

相关位置：

- [registry.go](/home/qingke/AstraStorage/internal/platform/observability/metrics/registry.go)
- [logger.go](/home/qingke/AstraStorage/internal/platform/observability/logging/logger.go)
- [observability.go](/home/qingke/AstraStorage/internal/gateway/observability.go)
- [observability.go](/home/qingke/AstraStorage/internal/mds/observability.go)
- [observability.go](/home/qingke/AstraStorage/internal/datanode/observability.go)

当前为什么这样做：

- 先把当前 MVP 的应用层行为看清楚
- 先解决 upload / download / delete / repair / replicate / heartbeat 的可观测性
- 不在还没上 Docker / Kubernetes 前就把平台监控体系做大

风险是什么：

- 没有 tracing backend，跨服务排障仍以 request id + 日志为主
- 没有 dashboard 和告警，指标虽然存在，但还没有自动消费层
- 没有 PostgreSQL / Redis / RabbitMQ / Kubernetes 基础设施 exporter
- datanode 生命周期 gauge 目前仍偏单节点视角，不是集群级状态模型

什么时候该还：

- 在准备接 Docker / Kubernetes 之前就应该先规划 dashboard 和抓取方式
- 在引入 Redis / RabbitMQ / PostgreSQL 集群后，必须补依赖层和基础设施层监控

还债顺序：

- 顺序 `12`

建议增强方向：

- 先给当前指标补 dashboard 和最小告警
- 再接 tracing
- 然后补 PostgreSQL / Redis / RabbitMQ / Kubernetes 的 exporter 与采集配置

---

## 当前最重要的结论

从现在的阶段看，最值得优先偿还的技术债不是数据库 schema，也不是 gRPC/HTTP 协议层，而是：

1. `gateway` 多 chunk 与流式数据链路
2. `gateway + datanode` 的删除与失败清理闭环
3. `MDS` 的真实 placement 与后台恢复逻辑

也就是说，当前项目最需要增强的不是“抽象够不够漂亮”，而是“数据面能力是否已经从 MVP 迈向稳定闭环”。
