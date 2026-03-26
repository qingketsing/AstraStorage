# MDS gRPC API

## Overview

在 HTTP 第一版接口之外，MDS 现在还提供第二套 `gRPC` 接口。
gRPC 层复用现有 `handler -> service -> store` 业务链路，只增加协议适配和错误码映射。

相关代码位置：

- `internal/mds/grpcpb/mds.proto`
- `internal/mds/grpcpb/mds.pb.go`
- `internal/mds/grpcpb/mds_grpc.pb.go`
- `internal/mds/rpc/grpc.go`

## Enable gRPC

默认情况下，MDS 会启动 HTTP 服务。
当设置 `MDS_GRPC_ADDR` 时，会额外启动 gRPC 服务。

例如：

```bash
MDS_HTTP_ADDR=:8080 \
MDS_GRPC_ADDR=:9090 \
go run ./cmd/mds
```

## Proto Contract

proto 文件位于：

```text
internal/mds/grpcpb/mds.proto
```

当前 `MetadataService` 覆盖这些方法：

- `Health`
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

## Error Mapping

当前 gRPC 状态码映射如下：

| Store Error | gRPC Code |
| --- | --- |
| `store.ErrInvalidArgument` | `InvalidArgument` |
| `store.ErrNotFound` | `NotFound` |
| `store.ErrAlreadyExists` | `AlreadyExists` |
| `store.ErrConflict` | `FailedPrecondition` |
| health check backend unavailable | `Unavailable` |
| other internal errors | `Internal` |

## Testing

gRPC transport 使用 `bufconn` 做无真实端口的进程内测试，覆盖上传主链路和错误码映射。
对应测试文件：

- `internal/mds/rpc/grpc_test.go`
