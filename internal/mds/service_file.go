package mds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// CreateFileRequest 描述一次文件创建请求。
type CreateFileRequest struct {
	InodeID       metadata.InodeID
	FileID        metadata.FileID
	ParentID      metadata.InodeID
	Name          string
	Size          int64
	ContentType   string
	StorageClass  string
	UserMetadata  map[string]string
	Tags          map[string]string
	ReplicaPolicy *metadata.ReplicaPolicy
	CreatedAt     time.Time
}

// CreateFile 同时创建文件型 inode 和 FileMetadata。
func (s *Service) CreateFile(ctx context.Context, req CreateFileRequest) (*metadata.FileMetadata, error) {
	if err := validateCreateFileRequest(req); err != nil {
		return nil, err
	}

	var created *metadata.FileMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		parent, err := tx.GetInode(ctx, store.InodeSelector{ID: req.ParentID})
		if err != nil {
			return err
		}
		if parent.Type != metadata.InodeTypeDirectory {
			return fmt.Errorf("%w: parent inode %q is not a directory", store.ErrInvalidArgument, parent.ID)
		}

		now := requestTime(req.CreatedAt)
		path := childPath(parent.Path, req.Name)
		inode := &metadata.InodeMetadata{
			ID:         req.InodeID,
			ParentID:   req.ParentID,
			FileID:     req.FileID,
			Name:       req.Name,
			Path:       path,
			Type:       metadata.InodeTypeFile,
			Status:     metadata.InodeStatusActive,
			Size:       req.Size,
			LinkCount:  1,
			Generation: 1,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		if err := tx.CreateInode(ctx, inode); err != nil {
			return err
		}

		file := &metadata.FileMetadata{
			ID:            req.FileID,
			InodeID:       req.InodeID,
			ParentInodeID: req.ParentID,
			Path:          path,
			Name:          req.Name,
			Size:          req.Size,
			ChunkSize:     metadata.FixedChunkSizeBytes,
			Status:        metadata.FileStatusPending,
			ContentType:   req.ContentType,
			StorageClass:  req.StorageClass,
			UserMetadata:  cloneStringMap(req.UserMetadata),
			Tags:          cloneStringMap(req.Tags),
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if req.ReplicaPolicy != nil {
			file.ReplicaPolicy = *req.ReplicaPolicy
		} else {
			file.ReplicaPolicy = metadata.ReplicaPolicy{
				DesiredReplicaCount: metadata.DefaultReplicaCount,
				MinimumReplicaCount: metadata.MinimumReadableReplicaCount,
			}
		}
		if err := tx.CreateFile(ctx, file); err != nil {
			return err
		}
		created = cloneFile(file)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateFileReadModels(ctx, created.ID)
	s.invalidateDirectoryReadModels(ctx, created.ParentInodeID)
	return created, nil
}

// GetFile 查询单个文件元数据。
func (s *Service) GetFile(ctx context.Context, selector store.FileSelector) (*metadata.FileMetadata, error) {
	if s.readCache != nil && selector.ID != "" {
		return s.readCache.GetFile(ctx, selector.ID, func(ctx context.Context) (*metadata.FileMetadata, error) {
			return s.repo.GetFile(ctx, selector)
		})
	}
	return s.repo.GetFile(ctx, selector)
}

func validateCreateFileRequest(req CreateFileRequest) error {
	if req.InodeID == "" || req.FileID == "" || req.ParentID == "" {
		return fmt.Errorf("%w: inode id, file id and parent id are required", store.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("%w: file name is required", store.ErrInvalidArgument)
	}
	if req.Size < 0 {
		return fmt.Errorf("%w: file size cannot be negative", store.ErrInvalidArgument)
	}
	return nil
}
