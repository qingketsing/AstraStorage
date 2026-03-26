package mds

import (
	"context"
	"fmt"
	"slices"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// StartUploadRequest 描述一次上传会话初始化请求。
type StartUploadRequest struct {
	SessionID           metadata.UploadSessionID
	FileID              metadata.FileID
	UploadKey           string
	ExpectedSize        int64
	ExpectedChecksum    *metadata.Checksum
	ClientMetadata      map[string]string
	TransportAttributes map[string]string
	ExpiresAt           *time.Time
	CreatedAt           time.Time
}

// CommitChunkRequest 描述一次分片提交请求。
type CommitChunkRequest struct {
	SessionID     metadata.UploadSessionID
	ChunkID       metadata.ChunkID
	Index         int64
	Offset        int64
	Size          int64
	Status        metadata.ChunkStatus
	Checksum      *metadata.Checksum
	Replicas      metadata.ReplicaSet
	ReplicaPolicy *metadata.ReplicaPolicy
	CommittedAt   time.Time
}

// CompleteUploadRequest 描述上传完成请求。
type CompleteUploadRequest struct {
	SessionID        metadata.UploadSessionID
	FinalChecksum    *metadata.Checksum
	ExpectedStatuses []metadata.FileStatus
	CompletedAt      time.Time
}

// VerifyUploadRequest 描述上传校验完成请求。
type VerifyUploadRequest struct {
	SessionID        metadata.UploadSessionID
	VerifiedChecksum *metadata.Checksum
	ExpectedStatuses []metadata.FileStatus
	VerifiedAt       time.Time
}

// FailUploadVerificationRequest 描述一次显式的校验失败回写。
type FailUploadVerificationRequest struct {
	SessionID        metadata.UploadSessionID
	ChunkID          metadata.ChunkID
	ActualChecksum   *metadata.Checksum
	ExpectedStatuses []metadata.FileStatus
	ErrorCode        string
	ErrorMessage     string
	Retryable        bool
	Attempt          int
	MaxAttempts      int
	FailedAt         time.Time
	NextRetryAt      *time.Time
}

// RetryUploadRequest 描述一次上传重试恢复请求。
type RetryUploadRequest struct {
	SessionID        metadata.UploadSessionID
	ExpectedStatuses []metadata.FileStatus
	RetriedAt        time.Time
}

// StartUpload 创建上传会话，并把文件状态推进到 uploading。
func (s *Service) StartUpload(ctx context.Context, req StartUploadRequest) (*metadata.UploadSession, error) {
	if err := validateStartUploadRequest(req); err != nil {
		return nil, err
	}

	var created *metadata.UploadSession
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		file, err := tx.GetFile(ctx, store.FileSelector{ID: req.FileID})
		if err != nil {
			return err
		}
		if file.Status == metadata.FileStatusDeleting || file.Status == metadata.FileStatusDeleted {
			return fmt.Errorf("%w: file %q cannot start upload in status %q", store.ErrConflict, file.ID, file.Status)
		}
		if err := ensureNoActiveUploadSession(ctx, tx, file.ID); err != nil {
			return err
		}

		now := requestTime(req.CreatedAt)
		session := &metadata.UploadSession{
			ID:                  req.SessionID,
			FileID:              req.FileID,
			UploadKey:           req.UploadKey,
			Status:              metadata.UploadStatusPending,
			ExpectedSize:        req.ExpectedSize,
			ChunkSize:           metadata.FixedChunkSizeBytes,
			ExpectedChecksum:    cloneChecksumPtr(req.ExpectedChecksum),
			CreatedAt:           now,
			UpdatedAt:           now,
			ExpiresAt:           cloneTimePtr(req.ExpiresAt),
			ClientMetadata:      cloneStringMap(req.ClientMetadata),
			TransportAttributes: cloneStringMap(req.TransportAttributes),
		}
		if err := tx.CreateUploadSession(ctx, session); err != nil {
			return err
		}

		status := metadata.FileStatusUploading
		if err := tx.UpdateFile(ctx, store.FilePatch{
			Selector:              store.FileSelector{ID: file.ID},
			Status:                &status,
			Size:                  int64Ptr(req.ExpectedSize),
			LatestUploadSessionID: uploadSessionIDPtr(session.ID),
			UpdatedAt:             now,
		}); err != nil {
			return err
		}
		created = cloneUploadSession(session)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, created.FileID)
	return created, nil
}

// CommitChunk 在一个事务里写入 chunk，并推进上传进度和文件已存储大小。
func (s *Service) CommitChunk(ctx context.Context, req CommitChunkRequest) (*metadata.ChunkMetadata, error) {
	if err := validateCommitChunkRequest(req); err != nil {
		return nil, err
	}

	var committed *metadata.ChunkMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		session, err := tx.GetUploadSession(ctx, req.SessionID)
		if err != nil {
			return err
		}
		if session.Status == metadata.UploadStatusCompleted {
			return fmt.Errorf("%w: upload session %q is already completed", store.ErrConflict, session.ID)
		}

		file, err := tx.GetFile(ctx, store.FileSelector{ID: session.FileID})
		if err != nil {
			return err
		}
		if file.LatestUploadSessionID != "" && file.LatestUploadSessionID != session.ID {
			return fmt.Errorf("%w: upload session %q is not the latest session for file %q", store.ErrConflict, session.ID, file.ID)
		}

		when := requestTime(req.CommittedAt)
		chunkStatus := req.Status
		if chunkStatus == "" {
			chunkStatus = metadata.ChunkStatusPersisted
		}
		replicaPolicy := file.ReplicaPolicy
		if req.ReplicaPolicy != nil {
			replicaPolicy = *req.ReplicaPolicy
		}
		chunk := metadata.ChunkMetadata{
			ID:            req.ChunkID,
			FileID:        session.FileID,
			Index:         req.Index,
			Offset:        req.Offset,
			Size:          req.Size,
			Status:        chunkStatus,
			Checksum:      derefChecksum(req.Checksum),
			ReplicaPolicy: replicaPolicy,
			ReplicaCount:  len(req.Replicas),
			Replicas:      cloneReplicaSet(req.Replicas),
			CreatedAt:     when,
			UpdatedAt:     when,
		}
		if err := tx.UpsertChunks(ctx, []metadata.ChunkMetadata{chunk}); err != nil {
			return err
		}

		confirmedOffset := req.Offset + req.Size
		progressStatus := metadata.UploadStatusActive
		if err := tx.UpdateUploadProgress(ctx, store.UploadProgressPatch{
			SessionID:            session.ID,
			Status:               progressStatus,
			ConfirmedOffset:      confirmedOffset,
			NextOffset:           confirmedOffset,
			LastPersistedChunkID: req.ChunkID,
			UpdatedAt:            when,
		}); err != nil {
			return err
		}

		storedSize, err := currentStoredSize(ctx, tx, session.FileID)
		if err != nil {
			return err
		}
		fileStatus := metadata.FileStatusUploading
		if err := tx.UpdateFile(ctx, store.FilePatch{
			Selector:   store.FileSelector{ID: file.ID},
			Status:     &fileStatus,
			StoredSize: &storedSize,
			UpdatedAt:  when,
		}); err != nil {
			return err
		}

		committedChunk, err := tx.GetChunk(ctx, store.ChunkSelector{ID: req.ChunkID})
		if err != nil {
			return err
		}
		committed = committedChunk
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, committed.FileID)
	return committed, nil
}

// CompleteUpload 在一个事务里封口写入流程，并把 file/session/chunk 推进到 verifying。
func (s *Service) CompleteUpload(ctx context.Context, req CompleteUploadRequest) (*metadata.FileMetadata, error) {
	if err := validateCompleteUploadRequest(req); err != nil {
		return nil, err
	}

	var completed *metadata.FileMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		session, err := tx.GetUploadSession(ctx, req.SessionID)
		if err != nil {
			return err
		}
		file, err := tx.GetFile(ctx, store.FileSelector{ID: session.FileID})
		if err != nil {
			return err
		}
		if len(req.ExpectedStatuses) > 0 && !slices.Contains(req.ExpectedStatuses, file.Status) {
			return fmt.Errorf("%w: file %q status %q is not allowed for completion", store.ErrConflict, file.ID, file.Status)
		}

		when := requestTime(req.CompletedAt)
		storedSize, err := currentStoredSize(ctx, tx, file.ID)
		if err != nil {
			return err
		}
		if err := ensureFileReadyForVerification(ctx, tx, file, session, storedSize); err != nil {
			return err
		}

		progress := store.UploadProgressPatch{
			SessionID:            session.ID,
			Status:               metadata.UploadStatusVerifying,
			ConfirmedOffset:      session.ConfirmedOffset,
			NextOffset:           session.NextOffset,
			LastPersistedChunkID: session.LastPersistedChunk,
			UpdatedAt:            when,
		}
		if req.FinalChecksum != nil {
			if checksumReady(*req.FinalChecksum) {
				progress.VerifiedChecksum = cloneChecksumPtr(req.FinalChecksum)
			} else {
				progress.ExpectedChecksum = cloneChecksumPtr(req.FinalChecksum)
			}
		}
		if err := tx.UpdateUploadProgress(ctx, progress); err != nil {
			return err
		}

		chunks, err := tx.ListChunksByFile(ctx, file.ID)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if err := tx.UpdateChunkStatus(ctx, store.ChunkStatusPatch{
				Selector:   store.ChunkSelector{ID: chunk.ID},
				Status:     metadata.ChunkStatusVerifying,
				Checksum:   &chunk.Checksum,
				VerifiedAt: chunk.Checksum.VerifiedAt,
				UpdatedAt:  when,
			}); err != nil {
				return err
			}
		}

		status := metadata.FileStatusVerifying
		size := file.Size
		if session.ExpectedSize > 0 {
			size = session.ExpectedSize
		}

		patch := store.FilePatch{
			Selector:   store.FileSelector{ID: file.ID},
			Status:     &status,
			Size:       &size,
			StoredSize: &storedSize,
			UpdatedAt:  when,
		}
		if req.FinalChecksum != nil && checksumReady(*req.FinalChecksum) {
			patch.Checksum = cloneChecksumPtr(req.FinalChecksum)
		}
		if err := tx.UpdateFile(ctx, patch); err != nil {
			return err
		}

		completedFile, err := tx.GetFile(ctx, store.FileSelector{ID: file.ID})
		if err != nil {
			return err
		}
		completed = completedFile
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, completed.ID)
	return completed, nil
}

// VerifyUpload 在一个事务里完成最终校验，并把 file/session/chunk 推进到 available/completed。
func (s *Service) VerifyUpload(ctx context.Context, req VerifyUploadRequest) (*metadata.FileMetadata, error) {
	if err := validateVerifyUploadRequest(req); err != nil {
		return nil, err
	}

	var verified *metadata.FileMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		session, err := tx.GetUploadSession(ctx, req.SessionID)
		if err != nil {
			return err
		}
		file, err := tx.GetFile(ctx, store.FileSelector{ID: session.FileID})
		if err != nil {
			return err
		}
		if len(req.ExpectedStatuses) > 0 && !slices.Contains(req.ExpectedStatuses, file.Status) {
			return fmt.Errorf("%w: file %q status %q is not allowed for verification", store.ErrConflict, file.ID, file.Status)
		}

		when := requestTime(req.VerifiedAt)
		storedSize, err := currentStoredSize(ctx, tx, file.ID)
		if err != nil {
			return err
		}
		if err := ensureFileReadyForAvailability(ctx, tx, file, session, storedSize); err != nil {
			return err
		}

		checksum := selectVerifiedFileChecksum(req.VerifiedChecksum, session)
		if checksum == nil {
			return fmt.Errorf("%w: file %q is missing a verified checksum", store.ErrConflict, file.ID)
		}

		if req.VerifiedChecksum != nil {
			if err := tx.UpdateUploadProgress(ctx, store.UploadProgressPatch{
				SessionID:            session.ID,
				Status:               metadata.UploadStatusVerifying,
				ConfirmedOffset:      session.ConfirmedOffset,
				NextOffset:           session.NextOffset,
				LastPersistedChunkID: session.LastPersistedChunk,
				VerifiedChecksum:     cloneChecksumPtr(req.VerifiedChecksum),
				UpdatedAt:            when,
			}); err != nil {
				return err
			}
		}

		if err := tx.CompleteUploadSession(ctx, session.ID, when); err != nil {
			return err
		}

		chunks, err := tx.ListChunksByFile(ctx, file.ID)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if err := tx.UpdateChunkStatus(ctx, store.ChunkStatusPatch{
				Selector:   store.ChunkSelector{ID: chunk.ID},
				Status:     metadata.ChunkStatusAvailable,
				Checksum:   &chunk.Checksum,
				VerifiedAt: chunk.Checksum.VerifiedAt,
				UpdatedAt:  when,
			}); err != nil {
				return err
			}
		}

		status := metadata.FileStatusAvailable
		size := file.Size
		if session.ExpectedSize > 0 {
			size = session.ExpectedSize
		}
		if err := tx.UpdateFile(ctx, store.FilePatch{
			Selector:    store.FileSelector{ID: file.ID},
			Status:      &status,
			Size:        &size,
			StoredSize:  &storedSize,
			CompletedAt: &when,
			Checksum:    checksum,
			UpdatedAt:   when,
		}); err != nil {
			return err
		}

		verifiedFile, err := tx.GetFile(ctx, store.FileSelector{ID: file.ID})
		if err != nil {
			return err
		}
		verified = verifiedFile
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, verified.ID)
	return verified, nil
}

// FailUploadVerification 在一个事务里记录校验失败，并把 file/session/chunk 推进到 failed 或 retrying。
func (s *Service) FailUploadVerification(ctx context.Context, req FailUploadVerificationRequest) (*metadata.FileMetadata, error) {
	if err := validateFailUploadVerificationRequest(req); err != nil {
		return nil, err
	}

	var failed *metadata.FileMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		session, err := tx.GetUploadSession(ctx, req.SessionID)
		if err != nil {
			return err
		}
		file, err := tx.GetFile(ctx, store.FileSelector{ID: session.FileID})
		if err != nil {
			return err
		}
		if len(req.ExpectedStatuses) > 0 && !slices.Contains(req.ExpectedStatuses, file.Status) {
			return fmt.Errorf("%w: file %q status %q is not allowed for verification failure", store.ErrConflict, file.ID, file.Status)
		}

		when := requestTime(req.FailedAt)
		chunks, err := tx.ListChunksByFile(ctx, file.ID)
		if err != nil {
			return err
		}

		failedOffset := int64(0)
		if req.ChunkID != "" {
			chunk, err := tx.GetChunk(ctx, store.ChunkSelector{ID: req.ChunkID})
			if err != nil {
				return err
			}
			if chunk.FileID != file.ID {
				return fmt.Errorf("%w: chunk %q does not belong to file %q", store.ErrConflict, chunk.ID, file.ID)
			}
			failedOffset = chunk.Offset
		}

		if err := tx.RecordUploadFailure(ctx, store.UploadFailureRecord{
			SessionID:        session.ID,
			FileID:           file.ID,
			ChunkID:          req.ChunkID,
			FailedOffset:     failedOffset,
			ExpectedChecksum: cloneChecksumPtr(session.ExpectedChecksum),
			ActualChecksum:   cloneChecksumPtr(req.ActualChecksum),
			ErrorCode:        req.ErrorCode,
			ErrorMessage:     req.ErrorMessage,
			Retryable:        req.Retryable,
			Attempt:          req.Attempt,
			MaxAttempts:      req.MaxAttempts,
			OccurredAt:       when,
			NextRetryAt:      cloneTimePtr(req.NextRetryAt),
		}); err != nil {
			return err
		}

		for _, chunk := range chunks {
			checksum := &chunk.Checksum
			verifiedAt := chunk.Checksum.VerifiedAt
			if chunk.ID == req.ChunkID && req.ActualChecksum != nil {
				checksum = req.ActualChecksum
				verifiedAt = req.ActualChecksum.VerifiedAt
			}
			if err := tx.UpdateChunkStatus(ctx, store.ChunkStatusPatch{
				Selector:      store.ChunkSelector{ID: chunk.ID},
				Status:        metadata.ChunkStatusFailed,
				Checksum:      checksum,
				LastErrorCode: req.ErrorCode,
				VerifiedAt:    verifiedAt,
				UpdatedAt:     when,
			}); err != nil {
				return err
			}
		}

		status := metadata.FileStatusFailed
		emptyChecksum := metadata.Checksum{}
		storedSize, err := currentStoredSize(ctx, tx, file.ID)
		if err != nil {
			return err
		}
		if err := tx.UpdateFile(ctx, store.FilePatch{
			Selector:   store.FileSelector{ID: file.ID},
			Status:     &status,
			StoredSize: &storedSize,
			Checksum:   &emptyChecksum,
			UpdatedAt:  when,
		}); err != nil {
			return err
		}

		failedFile, err := tx.GetFile(ctx, store.FileSelector{ID: file.ID})
		if err != nil {
			return err
		}
		failed = failedFile
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, failed.ID)
	return failed, nil
}

// RetryUpload 在一个事务里把 retrying 会话恢复成可继续写入的状态。
func (s *Service) RetryUpload(ctx context.Context, req RetryUploadRequest) (*metadata.FileMetadata, error) {
	if err := validateRetryUploadRequest(req); err != nil {
		return nil, err
	}

	var retried *metadata.FileMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		session, err := tx.GetUploadSession(ctx, req.SessionID)
		if err != nil {
			return err
		}
		file, err := tx.GetFile(ctx, store.FileSelector{ID: session.FileID})
		if err != nil {
			return err
		}
		if len(req.ExpectedStatuses) > 0 && !slices.Contains(req.ExpectedStatuses, file.Status) {
			return fmt.Errorf("%w: file %q status %q is not allowed for retry", store.ErrConflict, file.ID, file.Status)
		}
		if session.Status != metadata.UploadStatusRetrying {
			return fmt.Errorf("%w: upload session %q status %q is not retrying", store.ErrConflict, session.ID, session.Status)
		}

		when := requestTime(req.RetriedAt)
		restartOffset := session.Retry.LastFailedOffset
		chunks, err := tx.ListChunksByFile(ctx, file.ID)
		if err != nil {
			return err
		}

		var lastPersistedChunk metadata.ChunkID
		for _, chunk := range chunks {
			if chunk.Offset < restartOffset {
				if err := tx.UpdateChunkStatus(ctx, store.ChunkStatusPatch{
					Selector:   store.ChunkSelector{ID: chunk.ID},
					Status:     metadata.ChunkStatusPersisted,
					Checksum:   &chunk.Checksum,
					VerifiedAt: chunk.Checksum.VerifiedAt,
					UpdatedAt:  when,
				}); err != nil {
					return err
				}
				lastPersistedChunk = chunk.ID
				continue
			}
			if err := tx.DeleteChunk(ctx, store.ChunkSelector{ID: chunk.ID}); err != nil {
				return err
			}
		}

		storedSize, err := currentStoredSize(ctx, tx, file.ID)
		if err != nil {
			return err
		}
		status := metadata.UploadStatusActive
		if err := tx.UpdateUploadProgress(ctx, store.UploadProgressPatch{
			SessionID:             session.ID,
			Status:                status,
			ConfirmedOffset:       restartOffset,
			NextOffset:            restartOffset,
			LastPersistedChunkID:  lastPersistedChunk,
			ClearVerifiedChecksum: true,
			UpdatedAt:             when,
		}); err != nil {
			return err
		}

		fileStatus := metadata.FileStatusUploading
		emptyChecksum := metadata.Checksum{}
		if err := tx.UpdateFile(ctx, store.FilePatch{
			Selector:   store.FileSelector{ID: file.ID},
			Status:     &fileStatus,
			StoredSize: &storedSize,
			Checksum:   &emptyChecksum,
			UpdatedAt:  when,
		}); err != nil {
			return err
		}

		retriedFile, err := tx.GetFile(ctx, store.FileSelector{ID: file.ID})
		if err != nil {
			return err
		}
		retried = retriedFile
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, retried.ID)
	return retried, nil
}

func validateStartUploadRequest(req StartUploadRequest) error {
	if req.SessionID == "" || req.FileID == "" {
		return fmt.Errorf("%w: session id and file id are required", store.ErrInvalidArgument)
	}
	if req.ExpectedSize < 0 {
		return fmt.Errorf("%w: expected size cannot be negative", store.ErrInvalidArgument)
	}
	return nil
}

func validateCommitChunkRequest(req CommitChunkRequest) error {
	if req.SessionID == "" || req.ChunkID == "" {
		return fmt.Errorf("%w: session id and chunk id are required", store.ErrInvalidArgument)
	}
	if req.Index < 0 || req.Offset < 0 || req.Size < 0 {
		return fmt.Errorf("%w: chunk index, offset and size must be non-negative", store.ErrInvalidArgument)
	}
	if err := validateChecksumShape(req.Checksum); err != nil {
		return err
	}
	return nil
}

func validateCompleteUploadRequest(req CompleteUploadRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("%w: session id is required", store.ErrInvalidArgument)
	}
	if err := validateChecksumShape(req.FinalChecksum); err != nil {
		return err
	}
	return nil
}

func validateVerifyUploadRequest(req VerifyUploadRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("%w: session id is required", store.ErrInvalidArgument)
	}
	if req.VerifiedChecksum != nil && !checksumReady(*req.VerifiedChecksum) {
		return fmt.Errorf("%w: verified checksum must be marked verified before finalize", store.ErrInvalidArgument)
	}
	return nil
}

func validateFailUploadVerificationRequest(req FailUploadVerificationRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("%w: session id is required", store.ErrInvalidArgument)
	}
	if req.ErrorCode == "" {
		return fmt.Errorf("%w: error code is required", store.ErrInvalidArgument)
	}
	if req.Attempt < 0 {
		return fmt.Errorf("%w: attempt cannot be negative", store.ErrInvalidArgument)
	}
	if req.MaxAttempts <= 0 {
		return fmt.Errorf("%w: max attempts must be positive", store.ErrInvalidArgument)
	}
	if err := validateChecksumShape(req.ActualChecksum); err != nil {
		return err
	}
	return nil
}

func validateRetryUploadRequest(req RetryUploadRequest) error {
	if req.SessionID == "" {
		return fmt.Errorf("%w: session id is required", store.ErrInvalidArgument)
	}
	return nil
}

func currentStoredSize(ctx context.Context, tx store.Tx, fileID metadata.FileID) (int64, error) {
	chunks, err := tx.ListChunksByFile(ctx, fileID)
	if err != nil {
		return 0, err
	}
	var storedSize int64
	for _, chunk := range chunks {
		if end := chunk.Offset + chunk.Size; end > storedSize {
			storedSize = end
		}
	}
	return storedSize, nil
}

func ensureNoActiveUploadSession(ctx context.Context, tx store.Tx, fileID metadata.FileID) error {
	sessions, err := tx.ListUploadSessionsByFile(ctx, fileID, "")
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if isTerminalUploadStatus(session.Status) {
			continue
		}
		return fmt.Errorf("%w: file %q already has active upload session %q", store.ErrConflict, fileID, session.ID)
	}
	return nil
}

func ensureFileReadyForVerification(ctx context.Context, tx store.Tx, file *metadata.FileMetadata, session *metadata.UploadSession, storedSize int64) error {
	chunks, err := tx.ListChunksByFile(ctx, file.ID)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("%w: file %q has no chunks", store.ErrConflict, file.ID)
	}

	expectedSize := file.Size
	if session.ExpectedSize > 0 {
		expectedSize = session.ExpectedSize
	}
	if storedSize != expectedSize {
		return fmt.Errorf("%w: stored size %d does not match expected size %d", store.ErrConflict, storedSize, expectedSize)
	}

	var nextOffset int64
	for idx, chunk := range chunks {
		if chunk.Offset != nextOffset {
			return fmt.Errorf("%w: chunk %q leaves a gap in file %q", store.ErrConflict, chunk.ID, file.ID)
		}
		if idx < len(chunks)-1 && chunk.Size != metadata.FixedChunkSizeBytes {
			return fmt.Errorf("%w: non-terminal chunk %q must have fixed size", store.ErrConflict, chunk.ID)
		}
		if chunk.Status == metadata.ChunkStatusCorrupted || chunk.Status == metadata.ChunkStatusFailed {
			return fmt.Errorf("%w: chunk %q is not readable", store.ErrConflict, chunk.ID)
		}
		nextOffset += chunk.Size
	}
	if nextOffset != expectedSize {
		return fmt.Errorf("%w: chunk coverage %d does not match expected size %d", store.ErrConflict, nextOffset, expectedSize)
	}
	return nil
}

func ensureFileReadyForAvailability(ctx context.Context, tx store.Tx, file *metadata.FileMetadata, session *metadata.UploadSession, storedSize int64) error {
	chunks, err := tx.ListChunksByFile(ctx, file.ID)
	if err != nil {
		return err
	}
	if err := ensureFileReadyForVerification(ctx, tx, file, session, storedSize); err != nil {
		return err
	}

	requiredReplicas := file.ReplicaPolicy.MinimumReplicaCount
	if requiredReplicas < 1 {
		requiredReplicas = metadata.MinimumReadableReplicaCount
	}

	for _, chunk := range chunks {
		if !checksumReady(chunk.Checksum) {
			return fmt.Errorf("%w: chunk %q checksum is not verified", store.ErrConflict, chunk.ID)
		}
		if healthyReplicaCount(chunk.Replicas) < requiredReplicas {
			return fmt.Errorf("%w: chunk %q does not satisfy minimum readable replicas", store.ErrConflict, chunk.ID)
		}
	}
	return nil
}

func validateChecksumShape(checksum *metadata.Checksum) error {
	if checksum == nil {
		return nil
	}
	if checksum.Verified && checksum.VerifiedAt == nil {
		return fmt.Errorf("%w: verified checksum requires verification time", store.ErrInvalidArgument)
	}
	return nil
}

func checksumReady(checksum metadata.Checksum) bool {
	return checksum.Verified && checksum.VerifiedAt != nil
}

func selectVerifiedFileChecksum(req *metadata.Checksum, session *metadata.UploadSession) *metadata.Checksum {
	if req != nil && checksumReady(*req) {
		return cloneChecksumPtr(req)
	}
	if session.VerifiedChecksum != nil && checksumReady(*session.VerifiedChecksum) {
		return cloneChecksumPtr(session.VerifiedChecksum)
	}
	return nil
}

func healthyReplicaCount(replicas metadata.ReplicaSet) int {
	count := 0
	for _, replica := range replicas {
		if replica.State == metadata.ReplicaStateReady {
			count++
		}
	}
	return count
}

func isTerminalUploadStatus(status metadata.UploadStatus) bool {
	switch status {
	case metadata.UploadStatusCompleted, metadata.UploadStatusFailed, metadata.UploadStatusExpired:
		return true
	default:
		return false
	}
}

func cloneReplicaSet(src metadata.ReplicaSet) metadata.ReplicaSet {
	if src == nil {
		return nil
	}
	dst := make(metadata.ReplicaSet, len(src))
	for nodeID, replica := range src {
		dst[nodeID] = replica
	}
	return dst
}

func derefChecksum(src *metadata.Checksum) metadata.Checksum {
	if src == nil {
		return metadata.Checksum{}
	}
	copyChecksum := *src
	if src.VerifiedAt != nil {
		t := *src.VerifiedAt
		copyChecksum.VerifiedAt = &t
	}
	return copyChecksum
}
