package mds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

// CreateDirectoryRequest 描述一次目录创建请求。
type CreateDirectoryRequest struct {
	InodeID     metadata.InodeID
	ParentID    metadata.InodeID
	Name        string
	Permissions uint32
	Owner       string
	Group       string
	CreatedAt   time.Time
}

// CreateDirectory 在指定父目录下创建一个目录型 inode。
func (s *Service) CreateDirectory(ctx context.Context, req CreateDirectoryRequest) (*metadata.InodeMetadata, error) {
	if err := validateCreateDirectoryRequest(req); err != nil {
		return nil, err
	}

	var created *metadata.InodeMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		parent, err := tx.GetInode(ctx, store.InodeSelector{ID: req.ParentID})
		if err != nil {
			return err
		}
		if parent.Type != metadata.InodeTypeDirectory {
			return fmt.Errorf("%w: parent inode %q is not a directory", store.ErrInvalidArgument, parent.ID)
		}

		now := requestTime(req.CreatedAt)
		inode := &metadata.InodeMetadata{
			ID:          req.InodeID,
			ParentID:    req.ParentID,
			Name:        req.Name,
			Path:        childPath(parent.Path, req.Name),
			Type:        metadata.InodeTypeDirectory,
			Status:      metadata.InodeStatusActive,
			Permissions: req.Permissions,
			Owner:       req.Owner,
			Group:       req.Group,
			LinkCount:   1,
			Generation:  1,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := tx.CreateInode(ctx, inode); err != nil {
			return err
		}
		created = cloneInode(inode)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.invalidateDirectoryReadModels(ctx, created.ParentID)
	return created, nil
}

// GetInode 查询单个 inode。
func (s *Service) GetInode(ctx context.Context, selector store.InodeSelector) (*metadata.InodeMetadata, error) {
	return s.repo.GetInode(ctx, selector)
}

func validateCreateDirectoryRequest(req CreateDirectoryRequest) error {
	if req.InodeID == "" || req.ParentID == "" {
		return fmt.Errorf("%w: inode id and parent id are required", store.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("%w: directory name is required", store.ErrInvalidArgument)
	}
	return nil
}
