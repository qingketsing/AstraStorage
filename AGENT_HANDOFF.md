# AGENT_HANDOFF

Last updated: 2026-03-18

## Scope

This handoff captures only what is explicitly recoverable from the repository at `/home/qingke/AstraStorage`.
There is no Git history available in this workspace, so prior agent intent, commit history, and PR context are not treated as facts here.

## Project Goal

Verified facts:
- `AstraStorage` is intended to be a distributed cloud storage system for Kubernetes-oriented environments.
- The current implementation focus is the metadata control plane, especially MDS.
- MDS is responsible for inode tree metadata, file metadata, chunk metadata, replica/node metadata, and upload session state.

Primary references:
- `docs/architecture/system-overview.md`
- `docs/architecture/mds-overview.md`

## Current Implemented State

Verified facts:
- The active executable is `cmd/mds`.
- `cmd/mds/app.go` assembles `memory repository -> service -> handler -> in-process rpc router`.
- The repository implementation in active use is the in-memory store.
- The current MDS supports these in-process flows:
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
  - `GetUploadSession`
  - `ListChildren`
  - `ListFileChunks`
  - `BuildDownloadPlan`

Verified code references:
- `cmd/mds/app.go`
- `internal/mds/service_upload.go`
- `internal/mds/service_mutation.go`
- `internal/mds/handler.go`
- `internal/mds/rpc/types.go`
- `internal/mds/rpc/router.go`

## Upload State Machine

Verified facts:
- `StartUpload` rejects concurrent non-terminal sessions for the same file.
- `CompleteUpload` moves file/session/chunks into `verifying`.
- `VerifyUpload` requires verified file checksum, verified chunk checksums, and minimum readable replicas before promoting to `available`.
- `FailUploadVerification` records verification failure and pushes the session to `failed` or `retrying`.
- `RetryUpload` reopens the upload window from the last failed offset and clears stale verified checksum state.

Primary reference:
- `internal/mds/service_upload.go`

## Consistency Work Already Done

Verified facts:
- File rename and move synchronize inode and file metadata.
- Directory rename and move update subtree paths.
- File deletion cascades into chunk and upload-session cleanup.
- Recursive directory deletion cascades through nested children.

Primary reference:
- `internal/mds/service_mutation.go`

## Testing Status

Verified facts:
- `go test ./...` currently passes using `GOCACHE=/tmp/astra-go-build-cache`.
- Tests cover upload lifecycle, verification failure and retry lifecycle, rename/move/delete flows, and download plan generation.

Observed latest test result:

```text
ok  	AstraStorage/cmd/mds	(cached)
ok  	AstraStorage/internal/mds	(cached)
?   	AstraStorage/internal/mds/config	[no test files]
?   	AstraStorage/internal/mds/coordinator	[no test files]
?   	AstraStorage/internal/mds/discovery	[no test files]
?   	AstraStorage/internal/mds/metadata	[no test files]
?   	AstraStorage/internal/mds/placement	[no test files]
ok  	AstraStorage/internal/mds/rpc	(cached)
ok  	AstraStorage/internal/mds/store	(cached)
```

Primary references:
- `internal/mds/service_test.go`
- `internal/mds/rpc/router_test.go`

## Important Design Decisions

Verified facts:
- `inode` and `file/chunk` responsibilities are separated.
- `Path` is treated as cached metadata, not the source of truth.
- Chunk size is fixed at `4 MiB`.
- Upload process state is modeled separately in `UploadSession`.
- The current rpc layer is intentionally in-process to stabilize method contracts before adding HTTP/gRPC.
- The memory store is a development/test substrate, not a production backend.

Primary references:
- `docs/architecture/mds-overview.md`
- `docs/architecture/mds-rpc.md`
- `docs/architecture/mds-memory-store.md`

## Risks And Gaps

Verified facts:
- The memory transaction model is snapshot-copy plus whole-state replace, with no real concurrency conflict detection.
- There is no real network server yet.
- There is no PostgreSQL or other persistent backend yet.
- `placement`, `discovery`, and `coordinator` remain placeholders.
- Async verifier jobs, background retry scheduling, and health aggregation loops are not implemented.

Inference based on current docs and code:
- The upload semantics are far enough along that async verifier/retry orchestration is the next logical step before network/server or PostgreSQL work.

Primary references:
- `internal/mds/store/memory_tx.go`
- `docs/architecture/mds-implementation.md`
- `docs/architecture/mds-overview.md`

## Facts Vs Inference

Facts:
- Everything listed above under "Verified facts".

Inference:
- The likely next implementation priority is:
  1. async verifier execution
  2. background retry scheduling
  3. health writeback
  4. persistent backend
  5. external transport layer

Reason:
- This ordering is stated in architecture docs, but there is no active task tracker or commit history confirming that work has already started.

## Recommended Next Step

Recommended next implementation step:
- Add an explicit async verification workflow with TDD first.

Suggested scope:
- Introduce a verifier-facing service boundary that processes `verifying` sessions asynchronously.
- Add retry scheduling semantics around `NextRetryAt`.
- Preserve the current synchronous service API until async orchestration is stable.

Suggested files to inspect first:
- `internal/mds/service_upload.go`
- `internal/mds/store/store.go`
- `internal/mds/store/memory_upload.go`
- `internal/mds/service_test.go`
- `internal/mds/rpc/router_test.go`

## Workspace Notes

Verified facts:
- This workspace is not a Git repository.
- No `README.md`, `DECISIONS.md`, `TASKS.md`, `KNOWN_ISSUES.md`, or prior `AGENT_HANDOFF.md` were present at handoff time.
- No `TODO`, `FIXME`, `XXX`, or `HACK` markers were found in the repository during handoff review.
