# MDS HTTP API

## Overview

当前 MDS 对外第一版接口采用 `JSON over HTTP`。
HTTP 层只负责网络传输、JSON 编解码和错误码映射，核心业务仍然复用 `handler -> service -> store` 这条链路。

实现入口：

- `GET /healthz`
- `POST /rpc/<method>`

核心代码位置：

- `internal/mds/rpc/http.go`
- `internal/mds/rpc/router.go`
- `internal/mds/rpc/types.go`

## Transport Contract

### Health Check

```http
GET /healthz
```

成功响应：

```json
{
  "status": "ok"
}
```

### RPC Dispatch

```http
POST /rpc/<method>
Content-Type: application/json
```

请求体直接使用对应 method 的请求结构 JSON。
成功时响应体直接返回对应的响应结构 JSON，不再额外包一层 `data` 字段。

例如：

```http
POST /rpc/mds.get_file
Content-Type: application/json
```

```json
{
  "ID": "demo-file"
}
```

## Error Model

失败响应统一返回：

```json
{
  "error": {
    "code": "invalid_argument",
    "message": "store: invalid argument: ..."
  }
}
```

当前错误码映射如下：

| Store Error | HTTP Status | Error Code |
| --- | --- | --- |
| `store.ErrInvalidArgument` | `400` | `invalid_argument` |
| `store.ErrNotFound` | `404` | `not_found` |
| `store.ErrAlreadyExists` | `409` | `already_exists` |
| `store.ErrConflict` | `409` | `conflict` |
| unknown method | `404` | `unknown_method` |
| other internal errors | `500` | `internal` |

## Supported Methods

| Method | Request Type | Response Type |
| --- | --- | --- |
| `mds.create_directory` | `CreateDirectoryRequest` | `CreateDirectoryResponse` |
| `mds.create_file` | `CreateFileRequest` | `CreateFileResponse` |
| `mds.start_upload` | `StartUploadRequest` | `StartUploadResponse` |
| `mds.commit_chunk` | `CommitChunkRequest` | `CommitChunkResponse` |
| `mds.complete_upload` | `CompleteUploadRequest` | `CompleteUploadResponse` |
| `mds.verify_upload` | `VerifyUploadRequest` | `VerifyUploadResponse` |
| `mds.fail_upload_verification` | `FailUploadVerificationRequest` | `FailUploadVerificationResponse` |
| `mds.retry_upload` | `RetryUploadRequest` | `RetryUploadResponse` |
| `mds.rename_inode` | `RenameInodeRequest` | `RenameInodeResponse` |
| `mds.move_inode` | `MoveInodeRequest` | `MoveInodeResponse` |
| `mds.delete_file` | `DeleteFileRequest` | `DeleteFileResponse` |
| `mds.delete_directory` | `DeleteDirectoryRequest` | `DeleteDirectoryResponse` |
| `mds.get_inode` | `GetInodeRequest` | `GetInodeResponse` |
| `mds.get_file` | `GetFileRequest` | `GetFileResponse` |
| `mds.list_children` | `ListChildrenRequest` | `ListChildrenResponse` |
| `mds.list_file_chunks` | `ListFileChunksRequest` | `ListFileChunksResponse` |
| `mds.get_upload_session` | `GetUploadSessionRequest` | `GetUploadSessionResponse` |
| `mds.build_download_plan` | `BuildDownloadPlanRequest` | `BuildDownloadPlanResponse` |

这些结构的字段定义以 `internal/mds/rpc/types.go` 为准。

## Example Flow

### 1. Create File

```http
POST /rpc/mds.create_file
Content-Type: application/json
```

```json
{
  "InodeID": "video-inode",
  "FileID": "video-file",
  "ParentID": "root",
  "Name": "video.mp4",
  "Size": 4194560
}
```

### 2. Start Upload

```http
POST /rpc/mds.start_upload
Content-Type: application/json
```

```json
{
  "SessionID": "session-video",
  "FileID": "video-file",
  "ExpectedSize": 4194560
}
```

### 3. Commit Chunk

```http
POST /rpc/mds.commit_chunk
Content-Type: application/json
```

```json
{
  "SessionID": "session-video",
  "ChunkID": "session-video-chunk-0",
  "Index": 0,
  "Offset": 0,
  "Size": 4194304,
  "Checksum": {
    "Algorithm": "sha256",
    "Value": "chunk-0",
    "Verified": true
  },
  "Replicas": {
    "node-a": {
      "NodeID": "node-a",
      "Role": "primary",
      "State": "ready"
    }
  }
}
```

### 4. Build Download Plan

```http
POST /rpc/mds.build_download_plan
Content-Type: application/json
```

```json
{
  "FileID": "video-file"
}
```
