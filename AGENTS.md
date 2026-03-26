# Repository Guidelines

## Project Structure & Module Organization

`AstraStorage` is a Go monorepo centered on metadata services for distributed storage. Keep executable entrypoints in `cmd/` (`cmd/mds/` is the active binary; `cmd/gateway/` and `cmd/monitor/` are reserved). Core MDS domain code lives in `internal/mds/`, split by concern: `metadata/` for domain models, `store/` for repository and transaction interfaces, `rpc/` for transport types, and `placement/`, `discovery/`, `coordinator/` for future orchestration logic. Architecture notes belong in `docs/architecture/`. Deployment manifests go under `deploy/`. Put integration assets and test data in `test/` (`test/integration/`, `test/e2e/`, `test/fixtures/`).

## Build, Test, and Development Commands

- `go test ./...`: compile and run all package tests.
- `go build ./...`: verify every package builds cleanly.
- `go run ./cmd/mds`: run the MDS entrypoint during local development.

Run commands from the repository root. Add package-specific commands to this file when new binaries or tooling are introduced.

## Coding Style & Naming Conventions

Use standard Go formatting and import ordering. Run `gofmt -w <file>` before submitting changes. Follow Go naming: exported identifiers use `PascalCase`, internal helpers use `camelCase`, and package names stay short and lowercase (`metadata`, `store`, `rpc`). Keep files focused on one concern; new domain types belong near related metadata models, not in `cmd/`. Prefer explicit interfaces and transaction boundaries over hidden global state.

## Testing Guidelines

Use Go’s `testing` package. Place unit tests next to the code they cover as `*_test.go`. Reserve `test/integration/` for storage-backed flows and `test/e2e/` for multi-component scenarios. Name tests with behavior-focused patterns such as `TestCreateInode_RejectsDuplicateName`. There is no stated coverage gate yet, but new repository logic should include both success and invariant-breaking cases.

## Commit & Pull Request Guidelines

Git history is not available in this workspace, so no repository-specific commit convention could be inferred. Use short imperative commit subjects, for example: `mds: add file placement patch validation`. Keep commits scoped to one concern. PRs should describe the problem, summarize the design, list validation steps (`go test ./...`, `go build ./...`), and link related issues or docs. Include request/response examples when changing RPC or metadata contracts.

## Architecture Notes

Read `PROJECT_STRUCTURE.md`, `docs/architecture/mds-invariants.md`, and `docs/architecture/mds-store.md` before changing interfaces or metadata models. These documents define the intended module boundaries and system invariants; code changes should preserve them.

Avoid degradation handling, fallback, hacks, heuristics, local stabilizations, or post-processing bandages that are not faithful general algorithms.