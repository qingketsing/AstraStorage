package repository

import (
	"context"
	"errors"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

var errNotImplemented = errors.New("postgres repository: metadata CRUD is not implemented yet")

type unsupportedRepository struct{}

func (unsupportedRepository) CreateInode(context.Context, *metadata.InodeMetadata) error {
	return errNotImplemented
}

func (unsupportedRepository) GetInode(context.Context, store.InodeSelector) (*metadata.InodeMetadata, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListChildren(context.Context, metadata.InodeID, store.ListOptions) ([]metadata.DirectoryEntry, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) UpdateInode(context.Context, store.InodePatch) error {
	return errNotImplemented
}

func (unsupportedRepository) MoveInode(context.Context, store.MoveInodeOperation) error {
	return errNotImplemented
}

func (unsupportedRepository) RenameInode(context.Context, store.RenameInodeOperation) error {
	return errNotImplemented
}

func (unsupportedRepository) DeleteInode(context.Context, store.InodeSelector) error {
	return errNotImplemented
}

func (unsupportedRepository) UpdateSubtreePaths(context.Context, store.UpdateSubtreePathsOperation) error {
	return errNotImplemented
}

func (unsupportedRepository) CreateFile(context.Context, *metadata.FileMetadata) error {
	return errNotImplemented
}

func (unsupportedRepository) GetFile(context.Context, store.FileSelector) (*metadata.FileMetadata, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListFiles(context.Context, store.FileFilter) ([]*metadata.FileMetadata, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) UpdateFile(context.Context, store.FilePatch) error {
	return errNotImplemented
}

func (unsupportedRepository) UpdateFilePlacements(context.Context, store.FilePlacementPatch) error {
	return errNotImplemented
}

func (unsupportedRepository) DeleteFile(context.Context, store.FileSelector) error {
	return errNotImplemented
}

func (unsupportedRepository) UpsertChunks(context.Context, []metadata.ChunkMetadata) error {
	return errNotImplemented
}

func (unsupportedRepository) GetChunk(context.Context, store.ChunkSelector) (*metadata.ChunkMetadata, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListChunksByFile(context.Context, metadata.FileID) ([]metadata.ChunkMetadata, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListChunksByNode(context.Context, metadata.NodeID) ([]metadata.ChunkMetadata, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) UpdateChunkStatus(context.Context, store.ChunkStatusPatch) error {
	return errNotImplemented
}

func (unsupportedRepository) UpdateChunkReplicas(context.Context, store.ChunkReplicaPatch) error {
	return errNotImplemented
}

func (unsupportedRepository) RemoveChunkReplica(context.Context, store.ChunkSelector, metadata.NodeID, time.Time) error {
	return errNotImplemented
}

func (unsupportedRepository) DeleteChunk(context.Context, store.ChunkSelector) error {
	return errNotImplemented
}

func (unsupportedRepository) CreateUploadSession(context.Context, *metadata.UploadSession) error {
	return errNotImplemented
}

func (unsupportedRepository) GetUploadSession(context.Context, metadata.UploadSessionID) (*metadata.UploadSession, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListUploadSessionsByFile(context.Context, metadata.FileID, metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) UpdateUploadProgress(context.Context, store.UploadProgressPatch) error {
	return errNotImplemented
}

func (unsupportedRepository) RecordUploadFailure(context.Context, store.UploadFailureRecord) error {
	return errNotImplemented
}

func (unsupportedRepository) CompleteUploadSession(context.Context, metadata.UploadSessionID, time.Time) error {
	return errNotImplemented
}

func (unsupportedRepository) DeleteUploadSession(context.Context, metadata.UploadSessionID) error {
	return errNotImplemented
}

func (unsupportedRepository) UpsertNode(context.Context, metadata.NodeInfo) error {
	return errNotImplemented
}

func (unsupportedRepository) GetNode(context.Context, metadata.NodeID) (*metadata.NodeInfo, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListNodes(context.Context, store.NodeFilter) ([]metadata.NodeInfo, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) UpdateNodeHeartbeat(context.Context, store.NodeHeartbeatPatch) error {
	return errNotImplemented
}

func (unsupportedRepository) CreateReplicaPlan(context.Context, *metadata.ReplicaPlan) error {
	return errNotImplemented
}

func (unsupportedRepository) GetReplicaPlan(context.Context, string) (*metadata.ReplicaPlan, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) ListReplicaPlans(context.Context, store.ReplicaPlanFilter) ([]metadata.ReplicaPlan, error) {
	return nil, errNotImplemented
}

func (unsupportedRepository) UpdateReplicaPlan(context.Context, store.ReplicaPlanPatch) error {
	return errNotImplemented
}

func (unsupportedRepository) DeleteReplicaPlan(context.Context, string) error {
	return errNotImplemented
}
