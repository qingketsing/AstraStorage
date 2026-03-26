# MDS 启动与组装说明

## 这份文档是干什么的

这份文档说明当前 `cmd/mds` 已经实现的启动和依赖组装逻辑：

- MDS 进程入口现在做了什么
- `repo -> service -> handler -> router` 是怎么接起来的
- 根目录 inode 如何在启动时初始化
- 当前启动入口的边界在哪里

对应代码在：

- [main.go](../../cmd/mds/main.go)
- [app.go](../../cmd/mds/app.go)
- [app_test.go](../../cmd/mds/app_test.go)

---

## 当前启动入口做了什么

`cmd/mds` 现在已经不是空壳，而是会完成一条最小依赖链的组装。

当前启动流程是：

1. 创建内存版 `Repository`
2. 检查根目录 inode 是否存在
3. 如果不存在，创建根目录 inode
4. 创建 `Service`
5. 创建 `Handler`
6. 创建 `RPC Router`
7. 把这些对象挂到 `application` 结构里

然后 `main()` 会输出启动完成信息。

---

## application 结构的作用

当前 `cmd/mds/app.go` 里有一个 `application` 结构，里面持有：

- `repo`
- `service`
- `handler`
- `router`

这样做的好处是：

- 进程入口不需要把所有依赖直接塞进 `main()`
- 启动装配逻辑可以单独测试
- 后续加配置、server、后台任务时，结构比较容易继续扩展

---

## 为什么启动时要确保根目录存在

MDS 的目录树是以根目录 inode 为起点的。

如果根目录不存在，后续这些动作都没法正常执行：

- 创建目录
- 创建文件
- 列目录
- 通过根路径做任何树结构操作

所以当前启动流程会先做一次：

- `GetInode(root)`
- 如果没找到，就 `CreateInode(root)`

这一步是最小系统初始化的一部分。

---

## 当前测试覆盖了什么

`app_test.go` 已经覆盖两类启动行为：

### 1. 依赖链是否组装完成

测试会检查：

- repo 不为空
- service 不为空
- handler 不为空
- router 不为空

同时也会检查根目录 inode 已经被初始化。

### 2. 启动后 router 是否可用

测试会直接通过 `application.router` 发起一次 `CreateFile` 请求，确认：

- 启动好的各层确实连通
- 不是“对象都创建了，但不能工作”

---

## 当前边界

现在的 `cmd/mds` 还只是最小启动器，不是完整 server。

它还没有：

- 配置加载
- HTTP / gRPC 监听
- 信号处理
- 后台任务启动
- 健康检查端点
- metrics / tracing 初始化

所以当前入口的准确定位是：

“已经能启动一个进程内可调用的 MDS 应用，但还没有对外网络暴露。”
