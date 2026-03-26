package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"AstraStorage/internal/mds/metadata"
)

func (r *memoryRepository) CreateUploadSession(_ context.Context, session *metadata.UploadSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return createUploadSession(&r.state, session)
}

func (tx *memoryTx) CreateUploadSession(_ context.Context, session *metadata.UploadSession) error {
	return createUploadSession(&tx.state, session)
}

// createUploadSession 初始化断点续传会话。
// 会话必须绑定到已存在的文件，并沿用固定 chunk size 约束。
func createUploadSession(state *memoryState, session *metadata.UploadSession) error {
	if session == nil {
		return fmt.Errorf("%w: session is nil", ErrInvalidArgument)
	}
	if session.ID == "" || session.FileID == "" {
		return fmt.Errorf("%w: session id and file id are required", ErrInvalidArgument)
	}
	if _, exists := state.uploadSessions[session.ID]; exists {
		return fmt.Errorf("%w: upload session id %q", ErrAlreadyExists, session.ID)
	}
	if _, ok := state.files[session.FileID]; !ok {
		return fmt.Errorf("%w: file %q", ErrNotFound, session.FileID)
	}
	if session.ChunkSize == 0 {
		session.ChunkSize = metadata.FixedChunkSizeBytes
	}
	if session.ChunkSize != metadata.FixedChunkSizeBytes {
		return fmt.Errorf("%w: chunk size must be %d", ErrInvalidArgument, metadata.FixedChunkSizeBytes)
	}
	copySession := cloneUploadSession(session)
	state.uploadSessions[copySession.ID] = copySession
	return nil
}

func (r *memoryRepository) GetUploadSession(_ context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return getUploadSession(r.state, sessionID)
}

func (tx *memoryTx) GetUploadSession(_ context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	return getUploadSession(tx.state, sessionID)
}

// getUploadSession 返回会话副本，避免外部直接篡改重试、checksum 等内部字段。
func getUploadSession(state memoryState, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	session, ok := state.uploadSessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: upload session", ErrNotFound)
	}
	return cloneUploadSession(session), nil
}

func (r *memoryRepository) ListUploadSessionsByFile(_ context.Context, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return listUploadSessionsByFile(r.state, fileID, status)
}

func (tx *memoryTx) ListUploadSessionsByFile(_ context.Context, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	return listUploadSessionsByFile(tx.state, fileID, status)
}

// listUploadSessionsByFile 支持按 file 和 status 查看会话集合，并按创建时间排序。
func listUploadSessionsByFile(state memoryState, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	sessions := make([]*metadata.UploadSession, 0)
	for _, session := range state.uploadSessions {
		if session.FileID != fileID {
			continue
		}
		if status != "" && session.Status != status {
			continue
		}
		sessions = append(sessions, cloneUploadSession(session))
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt.Before(sessions[j].CreatedAt) })
	return sessions, nil
}

func (r *memoryRepository) UpdateUploadProgress(_ context.Context, progress UploadProgressPatch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return updateUploadProgress(&r.state, progress)
}

func (tx *memoryTx) UpdateUploadProgress(_ context.Context, progress UploadProgressPatch) error {
	return updateUploadProgress(&tx.state, progress)
}

// updateUploadProgress 维护续传过程中的 offset、最近持久化 chunk 和校验进度。
// 已完成会话会被拒绝再次更新，避免完成态被破坏。
func updateUploadProgress(state *memoryState, progress UploadProgressPatch) error {
	session, ok := state.uploadSessions[progress.SessionID]
	if !ok {
		return fmt.Errorf("%w: upload session", ErrNotFound)
	}
	if session.Status == metadata.UploadStatusCompleted {
		return fmt.Errorf("%w: upload session %q is already completed", ErrConflict, session.ID)
	}
	if progress.ConfirmedOffset < 0 || progress.NextOffset < 0 || progress.ConfirmedOffset > progress.NextOffset {
		return fmt.Errorf("%w: invalid upload offsets", ErrInvalidArgument)
	}
	if expectedSize := uploadExpectedSize(*state, session); expectedSize > 0 && progress.NextOffset > expectedSize {
		return fmt.Errorf("%w: next offset exceeds expected size", ErrInvalidArgument)
	}
	session.Status = progress.Status
	session.ConfirmedOffset = progress.ConfirmedOffset
	session.NextOffset = progress.NextOffset
	session.LastPersistedChunk = progress.LastPersistedChunkID
	if progress.ClearExpectedChecksum {
		session.ExpectedChecksum = nil
	}
	if progress.ClearVerifiedChecksum {
		session.VerifiedChecksum = nil
	}
	if progress.ExpectedChecksum != nil {
		checksum := cloneChecksum(*progress.ExpectedChecksum)
		session.ExpectedChecksum = &checksum
	}
	if progress.VerifiedChecksum != nil {
		checksum := cloneChecksum(*progress.VerifiedChecksum)
		session.VerifiedChecksum = &checksum
	}
	if progress.TransportAttributes != nil {
		session.TransportAttributes = cloneStringMap(progress.TransportAttributes)
	}
	session.UpdatedAt = progress.UpdatedAt
	return nil
}

func (r *memoryRepository) RecordUploadFailure(_ context.Context, failure UploadFailureRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return recordUploadFailure(&r.state, failure)
}

func (tx *memoryTx) RecordUploadFailure(_ context.Context, failure UploadFailureRecord) error {
	return recordUploadFailure(&tx.state, failure)
}

// recordUploadFailure 把失败现场写回 RetryState，供重试决策或错误分析使用。
func recordUploadFailure(state *memoryState, failure UploadFailureRecord) error {
	session, ok := state.uploadSessions[failure.SessionID]
	if !ok {
		return fmt.Errorf("%w: upload session", ErrNotFound)
	}
	session.Status = metadata.UploadStatusFailed
	if failure.Retryable && failure.Attempt < failure.MaxAttempts {
		session.Status = metadata.UploadStatusRetrying
	}
	session.Retry.Attempt = failure.Attempt
	session.Retry.MaxAttempts = failure.MaxAttempts
	session.Retry.Retryable = failure.Retryable
	session.Retry.LastErrorCode = failure.ErrorCode
	session.Retry.LastErrorMessage = failure.ErrorMessage
	session.Retry.LastFailedOffset = failure.FailedOffset
	session.Retry.LastFailedChunk = failure.ChunkID
	if failure.NextRetryAt != nil {
		t := *failure.NextRetryAt
		session.Retry.NextRetryAt = &t
	}
	session.Retry.LastFailureAt = timePtr(failure.OccurredAt)
	session.UpdatedAt = failure.OccurredAt
	return nil
}

func (r *memoryRepository) CompleteUploadSession(_ context.Context, sessionID metadata.UploadSessionID, completedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return completeUploadSession(&r.state, sessionID, completedAt)
}

func (tx *memoryTx) CompleteUploadSession(_ context.Context, sessionID metadata.UploadSessionID, completedAt time.Time) error {
	return completeUploadSession(&tx.state, sessionID, completedAt)
}

// completeUploadSession 只负责切换完成状态和记录完成时间。
// 更严格的完成前检查，例如 ConfirmedOffset == 文件大小，需要后续继续补强。
func completeUploadSession(state *memoryState, sessionID metadata.UploadSessionID, completedAt time.Time) error {
	session, ok := state.uploadSessions[sessionID]
	if !ok {
		return fmt.Errorf("%w: upload session", ErrNotFound)
	}
	if expectedSize := uploadExpectedSize(*state, session); expectedSize > 0 && session.ConfirmedOffset != expectedSize {
		return fmt.Errorf("%w: confirmed offset %d does not match expected size %d", ErrConflict, session.ConfirmedOffset, expectedSize)
	}
	session.Status = metadata.UploadStatusCompleted
	session.CompletedAt = timePtr(completedAt)
	session.UpdatedAt = completedAt
	return nil
}

func uploadExpectedSize(state memoryState, session *metadata.UploadSession) int64 {
	if session.ExpectedSize > 0 {
		return session.ExpectedSize
	}
	file, ok := state.files[session.FileID]
	if !ok {
		return 0
	}
	if file.Size > 0 {
		return file.Size
	}
	return 0
}

func (r *memoryRepository) DeleteUploadSession(_ context.Context, sessionID metadata.UploadSessionID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return deleteUploadSession(&r.state, sessionID)
}

func (tx *memoryTx) DeleteUploadSession(_ context.Context, sessionID metadata.UploadSessionID) error {
	return deleteUploadSession(&tx.state, sessionID)
}

// deleteUploadSession 仅删除会话本身，不负责 file 或 chunk 的级联清理。
func deleteUploadSession(state *memoryState, sessionID metadata.UploadSessionID) error {
	if _, ok := state.uploadSessions[sessionID]; !ok {
		return fmt.Errorf("%w: upload session", ErrNotFound)
	}
	delete(state.uploadSessions, sessionID)
	return nil
}
