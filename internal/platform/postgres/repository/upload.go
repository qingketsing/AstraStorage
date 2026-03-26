package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"

	"github.com/jackc/pgx/v5"
)

const uploadSessionColumns = `
id,
file_id,
upload_key,
status,
expected_size,
chunk_size,
confirmed_offset,
next_offset,
last_persisted_chunk_id,
expected_checksum_algorithm,
expected_checksum_value,
expected_checksum_verified,
expected_checksum_verified_at,
verified_checksum_algorithm,
verified_checksum_value,
verified_checksum_verified,
verified_checksum_verified_at,
retry_attempt,
retry_max_attempts,
retryable,
last_error_code,
last_error_message,
last_failed_offset,
last_failed_chunk,
last_failure_at,
next_retry_at,
created_at,
updated_at,
expires_at,
completed_at,
client_metadata,
transport_attributes
`

func (r *Repository) CreateUploadSession(ctx context.Context, session *metadata.UploadSession) error {
	return createUploadSession(ctx, r.pool, session)
}

func (tx *Tx) CreateUploadSession(ctx context.Context, session *metadata.UploadSession) error {
	return createUploadSession(ctx, tx.tx, session)
}

func (r *Repository) GetUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	return getUploadSession(ctx, r.pool, sessionID)
}

func (tx *Tx) GetUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	return getUploadSession(ctx, tx.tx, sessionID)
}

func (r *Repository) ListUploadSessionsByFile(ctx context.Context, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	return listUploadSessionsByFile(ctx, r.pool, fileID, status)
}

func (tx *Tx) ListUploadSessionsByFile(ctx context.Context, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	return listUploadSessionsByFile(ctx, tx.tx, fileID, status)
}

func (r *Repository) UpdateUploadProgress(ctx context.Context, progress store.UploadProgressPatch) error {
	return updateUploadProgress(ctx, r.pool, progress)
}

func (tx *Tx) UpdateUploadProgress(ctx context.Context, progress store.UploadProgressPatch) error {
	return updateUploadProgress(ctx, tx.tx, progress)
}

func (r *Repository) RecordUploadFailure(ctx context.Context, failure store.UploadFailureRecord) error {
	return recordUploadFailure(ctx, r.pool, failure)
}

func (tx *Tx) RecordUploadFailure(ctx context.Context, failure store.UploadFailureRecord) error {
	return recordUploadFailure(ctx, tx.tx, failure)
}

func (r *Repository) CompleteUploadSession(ctx context.Context, sessionID metadata.UploadSessionID, completedAt time.Time) error {
	return completeUploadSession(ctx, r.pool, sessionID, completedAt)
}

func (tx *Tx) CompleteUploadSession(ctx context.Context, sessionID metadata.UploadSessionID, completedAt time.Time) error {
	return completeUploadSession(ctx, tx.tx, sessionID, completedAt)
}

func (r *Repository) DeleteUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) error {
	return deleteUploadSession(ctx, r.pool, sessionID)
}

func (tx *Tx) DeleteUploadSession(ctx context.Context, sessionID metadata.UploadSessionID) error {
	return deleteUploadSession(ctx, tx.tx, sessionID)
}

func createUploadSession(ctx context.Context, db queryDB, session *metadata.UploadSession) error {
	if session == nil {
		return fmt.Errorf("%w: session is nil", store.ErrInvalidArgument)
	}
	if session.ID == "" || session.FileID == "" {
		return fmt.Errorf("%w: session id and file id are required", store.ErrInvalidArgument)
	}
	if _, err := getFile(ctx, db, store.FileSelector{ID: session.FileID}); err != nil {
		return err
	}
	if session.ChunkSize == 0 {
		session.ChunkSize = metadata.FixedChunkSizeBytes
	}
	if session.ChunkSize != metadata.FixedChunkSizeBytes {
		return fmt.Errorf("%w: chunk size must be %d", store.ErrInvalidArgument, metadata.FixedChunkSizeBytes)
	}

	clientMetadata, err := marshalJSON(session.ClientMetadata, map[string]string{})
	if err != nil {
		return err
	}
	transportAttributes, err := marshalJSON(session.TransportAttributes, map[string]string{})
	if err != nil {
		return err
	}
	expectedAlgorithm, expectedValue, expectedVerified, expectedVerifiedAt := checksumFields(session.ExpectedChecksum)
	verifiedAlgorithm, verifiedValue, verifiedVerified, verifiedVerifiedAt := checksumFields(session.VerifiedChecksum)
	retryAttempt, retryMaxAttempts, retryable, lastErrorCode, lastErrorMessage, lastFailedOffset, lastFailedChunk, lastFailureAt, nextRetryAt := sessionRetryFields(session)

	_, err = db.Exec(ctx, `
INSERT INTO mds_upload_sessions (
	id, file_id, upload_key, status, expected_size, chunk_size, confirmed_offset, next_offset, last_persisted_chunk_id,
	expected_checksum_algorithm, expected_checksum_value, expected_checksum_verified, expected_checksum_verified_at,
	verified_checksum_algorithm, verified_checksum_value, verified_checksum_verified, verified_checksum_verified_at,
	retry_attempt, retry_max_attempts, retryable, last_error_code, last_error_message, last_failed_offset, last_failed_chunk,
	last_failure_at, next_retry_at, created_at, updated_at, expires_at, completed_at, client_metadata, transport_attributes
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31::jsonb, $32::jsonb)
`,
		string(session.ID),
		string(session.FileID),
		session.UploadKey,
		string(session.Status),
		session.ExpectedSize,
		session.ChunkSize,
		session.ConfirmedOffset,
		session.NextOffset,
		string(session.LastPersistedChunk),
		expectedAlgorithm,
		expectedValue,
		expectedVerified,
		expectedVerifiedAt,
		verifiedAlgorithm,
		verifiedValue,
		verifiedVerified,
		verifiedVerifiedAt,
		retryAttempt,
		retryMaxAttempts,
		retryable,
		lastErrorCode,
		lastErrorMessage,
		lastFailedOffset,
		lastFailedChunk,
		lastFailureAt,
		nextRetryAt,
		session.CreatedAt,
		session.UpdatedAt,
		session.ExpiresAt,
		session.CompletedAt,
		clientMetadata,
		transportAttributes,
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func getUploadSession(ctx context.Context, db queryDB, sessionID metadata.UploadSessionID) (*metadata.UploadSession, error) {
	session, err := scanUploadSession(db.QueryRow(ctx, "SELECT "+uploadSessionColumns+" FROM mds_upload_sessions WHERE id = $1 LIMIT 1", string(sessionID)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: upload session", store.ErrNotFound)
		}
		return nil, err
	}
	return session, nil
}

func listUploadSessionsByFile(ctx context.Context, db queryDB, fileID metadata.FileID, status metadata.UploadStatus) ([]*metadata.UploadSession, error) {
	query := "SELECT " + uploadSessionColumns + " FROM mds_upload_sessions WHERE file_id = $1"
	args := []any{string(fileID)}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", len(args)+1)
		args = append(args, string(status))
	}
	query += " ORDER BY created_at"

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres repository: list upload sessions query: %w", err)
	}
	defer rows.Close()

	sessions := make([]*metadata.UploadSession, 0)
	for rows.Next() {
		session, err := scanUploadSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres repository: iterate upload sessions: %w", err)
	}
	return sessions, nil
}

func updateUploadProgress(ctx context.Context, db queryDB, progress store.UploadProgressPatch) error {
	session, err := getUploadSession(ctx, db, progress.SessionID)
	if err != nil {
		return err
	}
	if session.Status == metadata.UploadStatusCompleted {
		return fmt.Errorf("%w: upload session %q is already completed", store.ErrConflict, session.ID)
	}
	if progress.ConfirmedOffset < 0 || progress.NextOffset < 0 || progress.ConfirmedOffset > progress.NextOffset {
		return fmt.Errorf("%w: invalid upload offsets", store.ErrInvalidArgument)
	}
	expectedSize, err := uploadExpectedSize(ctx, db, session)
	if err != nil {
		return err
	}
	if expectedSize > 0 && progress.NextOffset > expectedSize {
		return fmt.Errorf("%w: next offset exceeds expected size", store.ErrInvalidArgument)
	}

	expectedChecksum := session.ExpectedChecksum
	if progress.ClearExpectedChecksum {
		expectedChecksum = nil
	}
	if progress.ExpectedChecksum != nil {
		expectedChecksum = progress.ExpectedChecksum
	}

	verifiedChecksum := session.VerifiedChecksum
	if progress.ClearVerifiedChecksum {
		verifiedChecksum = nil
	}
	if progress.VerifiedChecksum != nil {
		verifiedChecksum = progress.VerifiedChecksum
	}

	transportAttributes := session.TransportAttributes
	if progress.TransportAttributes != nil {
		transportAttributes = progress.TransportAttributes
	}
	transportBytes, err := marshalJSON(transportAttributes, map[string]string{})
	if err != nil {
		return err
	}
	expectedAlgorithm, expectedValue, expectedVerified, expectedVerifiedAt := checksumFields(expectedChecksum)
	verifiedAlgorithm, verifiedValue, verifiedVerified, verifiedVerifiedAt := checksumFields(verifiedChecksum)

	_, err = db.Exec(ctx, `
UPDATE mds_upload_sessions
SET status = $1,
    confirmed_offset = $2,
    next_offset = $3,
    last_persisted_chunk_id = $4,
    expected_checksum_algorithm = $5,
    expected_checksum_value = $6,
    expected_checksum_verified = $7,
    expected_checksum_verified_at = $8,
    verified_checksum_algorithm = $9,
    verified_checksum_value = $10,
    verified_checksum_verified = $11,
    verified_checksum_verified_at = $12,
    transport_attributes = $13::jsonb,
    updated_at = $14
WHERE id = $15
`,
		string(progress.Status),
		progress.ConfirmedOffset,
		progress.NextOffset,
		string(progress.LastPersistedChunkID),
		expectedAlgorithm,
		expectedValue,
		expectedVerified,
		expectedVerifiedAt,
		verifiedAlgorithm,
		verifiedValue,
		verifiedVerified,
		verifiedVerifiedAt,
		transportBytes,
		progress.UpdatedAt,
		string(session.ID),
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func recordUploadFailure(ctx context.Context, db queryDB, failure store.UploadFailureRecord) error {
	session, err := getUploadSession(ctx, db, failure.SessionID)
	if err != nil {
		return err
	}
	status := metadata.UploadStatusFailed
	if failure.Retryable && failure.Attempt < failure.MaxAttempts {
		status = metadata.UploadStatusRetrying
	}

	_, err = db.Exec(ctx, `
UPDATE mds_upload_sessions
SET status = $1,
    retry_attempt = $2,
    retry_max_attempts = $3,
    retryable = $4,
    last_error_code = $5,
    last_error_message = $6,
    last_failed_offset = $7,
    last_failed_chunk = $8,
    last_failure_at = $9,
    next_retry_at = $10,
    updated_at = $9
WHERE id = $11
`,
		string(status),
		failure.Attempt,
		failure.MaxAttempts,
		failure.Retryable,
		failure.ErrorCode,
		failure.ErrorMessage,
		failure.FailedOffset,
		string(failure.ChunkID),
		failure.OccurredAt,
		failure.NextRetryAt,
		string(session.ID),
	)
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func completeUploadSession(ctx context.Context, db queryDB, sessionID metadata.UploadSessionID, completedAt time.Time) error {
	session, err := getUploadSession(ctx, db, sessionID)
	if err != nil {
		return err
	}
	expectedSize, err := uploadExpectedSize(ctx, db, session)
	if err != nil {
		return err
	}
	if expectedSize > 0 && session.ConfirmedOffset != expectedSize {
		return fmt.Errorf("%w: confirmed offset %d does not match expected size %d", store.ErrConflict, session.ConfirmedOffset, expectedSize)
	}
	_, err = db.Exec(ctx, `
UPDATE mds_upload_sessions
SET status = $1, completed_at = $2, updated_at = $2
WHERE id = $3
`, string(metadata.UploadStatusCompleted), completedAt, string(sessionID))
	if err != nil {
		return translateExecError(err)
	}
	return nil
}

func deleteUploadSession(ctx context.Context, db queryDB, sessionID metadata.UploadSessionID) error {
	if _, err := getUploadSession(ctx, db, sessionID); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `DELETE FROM mds_upload_sessions WHERE id = $1`, string(sessionID)); err != nil {
		return translateExecError(err)
	}
	return nil
}

func scanUploadSession(row rowScanner) (*metadata.UploadSession, error) {
	var session metadata.UploadSession
	var id string
	var fileID string
	var status string
	var lastPersistedChunkID string
	var expectedChecksumVerifiedAt sql.NullTime
	var verifiedChecksumVerifiedAt sql.NullTime
	var lastFailureAt sql.NullTime
	var nextRetryAt sql.NullTime
	var expiresAt sql.NullTime
	var completedAt sql.NullTime
	var clientMetadataBytes []byte
	var transportBytes []byte
	var expectedChecksumAlgorithm string
	var expectedChecksumValue string
	var expectedChecksumVerified bool
	var verifiedChecksumAlgorithm string
	var verifiedChecksumValue string
	var verifiedChecksumVerified bool
	var lastFailedChunk string

	if err := row.Scan(
		&id,
		&fileID,
		&session.UploadKey,
		&status,
		&session.ExpectedSize,
		&session.ChunkSize,
		&session.ConfirmedOffset,
		&session.NextOffset,
		&lastPersistedChunkID,
		&expectedChecksumAlgorithm,
		&expectedChecksumValue,
		&expectedChecksumVerified,
		&expectedChecksumVerifiedAt,
		&verifiedChecksumAlgorithm,
		&verifiedChecksumValue,
		&verifiedChecksumVerified,
		&verifiedChecksumVerifiedAt,
		&session.Retry.Attempt,
		&session.Retry.MaxAttempts,
		&session.Retry.Retryable,
		&session.Retry.LastErrorCode,
		&session.Retry.LastErrorMessage,
		&session.Retry.LastFailedOffset,
		&lastFailedChunk,
		&lastFailureAt,
		&nextRetryAt,
		&session.CreatedAt,
		&session.UpdatedAt,
		&expiresAt,
		&completedAt,
		&clientMetadataBytes,
		&transportBytes,
	); err != nil {
		return nil, err
	}

	session.ID = metadata.UploadSessionID(id)
	session.FileID = metadata.FileID(fileID)
	session.Status = metadata.UploadStatus(status)
	session.LastPersistedChunk = metadata.ChunkID(lastPersistedChunkID)
	session.Retry.LastFailedChunk = metadata.ChunkID(lastFailedChunk)
	session.ExpectedChecksum = checksumPtr(expectedChecksumAlgorithm, expectedChecksumValue, expectedChecksumVerified, expectedChecksumVerifiedAt)
	session.VerifiedChecksum = checksumPtr(verifiedChecksumAlgorithm, verifiedChecksumValue, verifiedChecksumVerified, verifiedChecksumVerifiedAt)
	if lastFailureAt.Valid {
		t := lastFailureAt.Time
		session.Retry.LastFailureAt = &t
	}
	if nextRetryAt.Valid {
		t := nextRetryAt.Time
		session.Retry.NextRetryAt = &t
	}
	if expiresAt.Valid {
		t := expiresAt.Time
		session.ExpiresAt = &t
	}
	if completedAt.Valid {
		t := completedAt.Time
		session.CompletedAt = &t
	}
	if err := unmarshalJSON(clientMetadataBytes, &session.ClientMetadata); err != nil {
		return nil, err
	}
	if err := unmarshalJSON(transportBytes, &session.TransportAttributes); err != nil {
		return nil, err
	}
	return &session, nil
}

func uploadExpectedSize(ctx context.Context, db queryDB, session *metadata.UploadSession) (int64, error) {
	if session.ExpectedSize > 0 {
		return session.ExpectedSize, nil
	}
	file, err := getFile(ctx, db, store.FileSelector{ID: session.FileID})
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return file.Size, nil
}

func checksumFields(checksum *metadata.Checksum) (string, string, bool, *time.Time) {
	if checksum == nil {
		return "", "", false, nil
	}
	return checksum.Algorithm, checksum.Value, checksum.Verified, checksum.VerifiedAt
}

func checksumPtr(algorithm, value string, verified bool, verifiedAt sql.NullTime) *metadata.Checksum {
	if algorithm == "" && value == "" && !verified && !verifiedAt.Valid {
		return nil
	}
	checksum := &metadata.Checksum{
		Algorithm: algorithm,
		Value:     value,
		Verified:  verified,
	}
	if verifiedAt.Valid {
		t := verifiedAt.Time
		checksum.VerifiedAt = &t
	}
	return checksum
}

func sessionRetryFields(session *metadata.UploadSession) (int, int, bool, string, string, int64, string, *time.Time, *time.Time) {
	return session.Retry.Attempt,
		session.Retry.MaxAttempts,
		session.Retry.Retryable,
		session.Retry.LastErrorCode,
		session.Retry.LastErrorMessage,
		session.Retry.LastFailedOffset,
		string(session.Retry.LastFailedChunk),
		session.Retry.LastFailureAt,
		session.Retry.NextRetryAt
}
