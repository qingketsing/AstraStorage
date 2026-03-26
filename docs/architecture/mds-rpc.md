# MDS RPC 层说明

## 这份文档是干什么的

这份文档说明当前仓库里已经实现的 `rpc` 层：

- 它现在实现到了哪一步
- method、request、response 是怎么组织的
- router 如何把请求转发到 handler
- 为什么当前只做进程内 router，而没有直接上网络协议

对应代码在：

- [types.go](../../internal/mds/rpc/types.go)
- [router.go](../../internal/mds/rpc/router.go)
- [router_test.go](../../internal/mds/rpc/router_test.go)

---

## 为什么先做进程内 RPC

当前仓库的目标不是马上选 HTTP 还是 gRPC，而是先稳定：

- method 名称
- request / response 结构
- 调用路径
- 错误传播行为

如果太早上真实网络层，后面业务还在变化时，协议会反复推翻。

所以现在的 `rpc` 层定位是：

- 定义传输结构
- 定义 method 常量
- 提供进程内 `Dispatch`
- 先把协议入口和业务调用面固定下来

---

## 当前整体结构

### types.go

当前已经定义了这些 method：

- `mds.create_directory`
- `mds.create_file`
- `mds.start_upload`
- `mds.commit_chunk`
- `mds.complete_upload`
- `mds.verify_upload`
- `mds.fail_upload_verification`
- `mds.retry_upload`
- `mds.rename_inode`
- `mds.move_inode`
- `mds.delete_file`
- `mds.delete_directory`
- `mds.get_inode`
- `mds.get_file`
- `mds.list_children`
- `mds.list_file_chunks`
- `mds.get_upload_session`
- `mds.build_download_plan`

每个 method 都有对应的 request / response 结构。

### router.go

`Router` 当前负责两件事：

1. `Dispatch`
   根据 method 名称做类型断言和请求分发
2. 每个 method 对应一个显式函数
   例如 `CreateFile`、`StartUpload`、`BuildDownloadPlan`

这样做的好处是：

- method 分发很清楚
- request 类型不容易混淆
- 后面接 HTTP / gRPC 时，映射关系不会重写

---

## 当前已经支持的能力

### 写路径

RPC 当前已经可以承接：

- 创建目录
- 创建文件
- 启动上传
- 提交 chunk
- 完成上传
- 校验上传
- 记录校验失败
- 恢复上传重试
- rename / move inode
- 删除文件
- 删除目录

### 读路径

RPC 当前已经可以承接：

- 查询 inode
- 查询 file
- 查询 upload session
- 列目录
- 列文件 chunk
- 生成下载计划

下载计划响应会把 service 内部的下载规划结构转换成 RPC 自己的响应类型，避免上层直接依赖内部 service 类型。

---

## 当前测试覆盖了什么

`router_test.go` 目前已经覆盖：

- 上传生命周期从 router 进入后能走通，包括 `complete -> verify`
- 校验失败和 retry 生命周期可以通过 `Dispatch` 调用
- 目录列举可以通过 `Dispatch` 调用
- 下载计划可以通过 `Dispatch` 调用
- rename / move / delete 这些高风险一致性流程可以通过 `Dispatch` 调用

这些测试主要验证：

- method 到 handler 的映射正确
- request / response 结构和断言正确
- 读写路径都能从 RPC 入口进入

---

## 当前边界

这层目前还不是“网络 RPC server”。

它还没有实现：

- HTTP 路由
- gRPC server
- 序列化协议
- 鉴权和中间件
- 版本协商

所以当前这层更准确地说是：

“MDS 的进程内协议入口和传输结构定义”。
