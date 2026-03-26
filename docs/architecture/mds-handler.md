# MDS Handler 层说明

## 这份文档是干什么的

这份文档说明当前仓库里已经落地的 `handler` 层：

- 它为什么存在
- 它和 `service`、`rpc` 的边界是什么
- 它现在已经暴露了哪些方法
- 它后续适合往哪里继续扩展

对应代码在：

- [handler.go](../../internal/mds/handler.go)

---

## 为什么要有 handler 层

当前 `service` 已经负责业务编排，但它还不是一个对外协议层。

如果将来要接：

- HTTP
- gRPC
- CLI
- 内部控制器调用

最好先有一层稳定的请求处理入口，把“业务动作”和“协议适配”之间隔开。

所以当前 handler 的定位是：

- 作为 service 的薄包装
- 给 RPC router 提供统一调用面
- 为后续补权限、审计、请求级日志留位置

---

## 当前整体结构

当前 `Handler` 非常薄，只持有一个 `Service`：

- `NewHandler`
- `CreateDirectory`
- `CreateFile`
- `StartUpload`
- `CommitChunk`
- `CompleteUpload`
- `VerifyUpload`
- `FailUploadVerification`
- `RetryUpload`
- `RenameInode`
- `MoveInode`
- `DeleteFile`
- `DeleteDirectory`
- `GetInode`
- `GetFile`
- `ListChildren`
- `ListFileChunks`
- `GetUploadSession`
- `BuildDownloadPlan`

它本身不保存状态，也不直接接触存储实现。

---

## 当前已经实现的能力

### 写接口

handler 已经支持转发这些写动作：

- 创建目录
- 创建文件
- 启动上传
- 提交 chunk
- 完成上传
- 校验上传并切换到 available
- 记录校验失败并切到 failed / retrying
- 恢复一次 retrying upload
- rename / move inode
- 删除文件
- 递归删除目录

这些方法当前都直接调用 service 同名方法，不额外改写事务或对象结构。

### 读接口

handler 已经支持转发这些读动作：

- 查询单个 inode
- 查询单个文件
- 查询上传会话
- 列目录
- 列文件 chunk
- 生成下载计划

其中 `BuildDownloadPlan` 是当前读路径里最接近真实下载入口的接口。

---

## 它和 service 层的关系

可以这样理解：

- `service`
  负责业务动作本身，决定事务边界和跨表更新
- `handler`
  负责把“外部请求”转成对 service 的调用

所以当前 handler 不做这些事情：

- 不重新拼装事务
- 不维护自己的状态
- 不直接操作 repository

这能保证调用路径比较稳定：

`router -> handler -> service -> store`

---

## 当前边界

现在这层还没有真正承担复杂逻辑。

后续更适合放到 handler 的内容包括：

- 权限检查
- 请求级别参数规范化
- 审计日志
- tracing / metrics 打点
- RPC / HTTP 错误到内部错误模型的转换

所以当前 handler 的角色更接近：

“协议层和业务层之间的一层薄适配器。”
