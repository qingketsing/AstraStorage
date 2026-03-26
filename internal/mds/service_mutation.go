package mds

import (
	"context"
	"fmt"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/store"
)

type RenameInodeRequest struct {
	InodeID   metadata.InodeID
	NewName   string
	UpdatedAt time.Time
}

type MoveInodeRequest struct {
	InodeID        metadata.InodeID
	TargetParentID metadata.InodeID
	NewName        string
	UpdatedAt      time.Time
}

type DeleteFileRequest struct {
	FileID    metadata.FileID
	DeletedAt time.Time
}

type DeleteDirectoryRequest struct {
	InodeID   metadata.InodeID
	Recursive bool
	DeletedAt time.Time
}

func (s *Service) RenameInode(ctx context.Context, req RenameInodeRequest) (*metadata.InodeMetadata, error) {
	if err := validateRenameInodeRequest(req); err != nil {
		return nil, err
	}

	var renamed *metadata.InodeMetadata
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		inode, err := tx.GetInode(ctx, store.InodeSelector{ID: req.InodeID})
		if err != nil {
			return err
		}
		oldPath := inode.Path
		when := requestTime(req.UpdatedAt)

		if err := tx.RenameInode(ctx, store.RenameInodeOperation{
			Selector:  store.InodeSelector{ID: req.InodeID},
			NewName:   req.NewName,
			UpdatedAt: when,
		}); err != nil {
			return err
		}

		updated, err := tx.GetInode(ctx, store.InodeSelector{ID: req.InodeID})
		if err != nil {
			return err
		}
		if err := syncInodeMutation(ctx, tx, inode, updated, oldPath, when); err != nil {
			return err
		}

		renamed = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	if renamed != nil {
		s.invalidateDirectoryReadModels(ctx, renamed.ParentID)
	}
	if renamed != nil && renamed.FileID != "" {
		s.invalidateFileReadModels(ctx, renamed.FileID)
	}
	return renamed, nil
}

func (s *Service) MoveInode(ctx context.Context, req MoveInodeRequest) (*metadata.InodeMetadata, error) {
	if err := validateMoveInodeRequest(req); err != nil {
		return nil, err
	}

	var moved *metadata.InodeMetadata
	var sourceParentID metadata.InodeID
	err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		inode, err := tx.GetInode(ctx, store.InodeSelector{ID: req.InodeID})
		if err != nil {
			return err
		}
		sourceParentID = inode.ParentID
		targetParent, err := tx.GetInode(ctx, store.InodeSelector{ID: req.TargetParentID})
		if err != nil {
			return err
		}
		if targetParent.Type != metadata.InodeTypeDirectory {
			return fmt.Errorf("%w: target parent inode %q is not a directory", store.ErrInvalidArgument, targetParent.ID)
		}

		oldPath := inode.Path
		when := requestTime(req.UpdatedAt)
		if err := tx.MoveInode(ctx, store.MoveInodeOperation{
			Selector:         store.InodeSelector{ID: req.InodeID},
			TargetParentID:   req.TargetParentID,
			TargetParentPath: targetParent.Path,
			NewName:          req.NewName,
			UpdatedAt:        when,
		}); err != nil {
			return err
		}

		updated, err := tx.GetInode(ctx, store.InodeSelector{ID: req.InodeID})
		if err != nil {
			return err
		}
		if err := syncInodeMutation(ctx, tx, inode, updated, oldPath, when); err != nil {
			return err
		}

		moved = updated
		return nil
	})
	if err != nil {
		return nil, err
	}
	if sourceParentID != "" {
		s.invalidateDirectoryReadModels(ctx, sourceParentID)
	}
	if moved != nil {
		s.invalidateDirectoryReadModels(ctx, moved.ParentID)
	}
	if moved != nil && moved.FileID != "" {
		s.invalidateFileReadModels(ctx, moved.FileID)
	}
	return moved, nil
}

func (s *Service) DeleteFile(ctx context.Context, req DeleteFileRequest) error {
	if err := validateDeleteFileRequest(req); err != nil {
		return err
	}

	var parentID metadata.InodeID
	if err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		file, err := tx.GetFile(ctx, store.FileSelector{ID: req.FileID})
		if err != nil {
			return err
		}
		parentID = file.ParentInodeID
		return deleteFileCascade(ctx, tx, req.FileID)
	}); err != nil {
		return err
	}
	s.invalidateFileReadModels(ctx, req.FileID)
	s.invalidateDirectoryReadModels(ctx, parentID)
	return nil
}

func (s *Service) DeleteDirectory(ctx context.Context, req DeleteDirectoryRequest) error {
	if err := validateDeleteDirectoryRequest(req); err != nil {
		return err
	}

	var parentID metadata.InodeID
	if err := s.repo.InTx(ctx, func(ctx context.Context, tx store.Tx) error {
		inode, err := tx.GetInode(ctx, store.InodeSelector{ID: req.InodeID})
		if err != nil {
			return err
		}
		parentID = inode.ParentID
		if inode.Type != metadata.InodeTypeDirectory {
			return fmt.Errorf("%w: inode %q is not a directory", store.ErrInvalidArgument, inode.ID)
		}
		if inode.ID == metadata.InodeID(metadata.RootInodeID) {
			return fmt.Errorf("%w: root directory cannot be deleted", store.ErrInvalidArgument)
		}
		if !req.Recursive {
			return tx.DeleteInode(ctx, store.InodeSelector{ID: inode.ID})
		}
		return deleteDirectoryRecursive(ctx, tx, inode.ID)
	}); err != nil {
		return err
	}
	s.invalidateDirectoryReadModels(ctx, parentID)
	return nil
}

func validateRenameInodeRequest(req RenameInodeRequest) error {
	if req.InodeID == "" {
		return fmt.Errorf("%w: inode id is required", store.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.NewName) == "" {
		return fmt.Errorf("%w: new name is required", store.ErrInvalidArgument)
	}
	return nil
}

func validateMoveInodeRequest(req MoveInodeRequest) error {
	if req.InodeID == "" || req.TargetParentID == "" {
		return fmt.Errorf("%w: inode id and target parent id are required", store.ErrInvalidArgument)
	}
	return nil
}

func validateDeleteFileRequest(req DeleteFileRequest) error {
	if req.FileID == "" {
		return fmt.Errorf("%w: file id is required", store.ErrInvalidArgument)
	}
	return nil
}

func validateDeleteDirectoryRequest(req DeleteDirectoryRequest) error {
	if req.InodeID == "" {
		return fmt.Errorf("%w: inode id is required", store.ErrInvalidArgument)
	}
	return nil
}

func syncInodeMutation(ctx context.Context, tx store.Tx, before, after *metadata.InodeMetadata, oldPath string, when time.Time) error {
	if before.Type == metadata.InodeTypeFile {
		return syncFileForInode(ctx, tx, after, when)
	}

	if err := tx.UpdateSubtreePaths(ctx, store.UpdateSubtreePathsOperation{
		RootID:    after.ID,
		OldPrefix: oldPath,
		NewPrefix: after.Path,
		UpdatedAt: when,
	}); err != nil {
		return err
	}
	return syncFilesUnderDirectoryPath(ctx, tx, oldPath, after.Path, when)
}

func syncFileForInode(ctx context.Context, tx store.Tx, inode *metadata.InodeMetadata, when time.Time) error {
	file, err := tx.GetFile(ctx, store.FileSelector{InodeID: inode.ID})
	if err != nil {
		return err
	}

	parentID := inode.ParentID
	path := inode.Path
	name := inode.Name
	return tx.UpdateFile(ctx, store.FilePatch{
		Selector:      store.FileSelector{ID: file.ID},
		ParentInodeID: &parentID,
		Path:          &path,
		Name:          &name,
		UpdatedAt:     when,
	})
}

func syncFilesUnderDirectoryPath(ctx context.Context, tx store.Tx, oldPrefix, newPrefix string, when time.Time) error {
	files, err := tx.ListFiles(ctx, store.FileFilter{PathPrefix: oldPrefix})
	if err != nil {
		return err
	}

	for _, file := range files {
		if !pathWithinPrefix(file.Path, oldPrefix) {
			continue
		}
		updatedPath := replacePathPrefix(file.Path, oldPrefix, newPrefix)
		if updatedPath == file.Path {
			continue
		}
		if err := tx.UpdateFile(ctx, store.FilePatch{
			Selector:  store.FileSelector{ID: file.ID},
			Path:      &updatedPath,
			UpdatedAt: when,
		}); err != nil {
			return err
		}
	}
	return nil
}

func deleteDirectoryRecursive(ctx context.Context, tx store.Tx, inodeID metadata.InodeID) error {
	children, err := tx.ListChildren(ctx, inodeID, store.ListOptions{})
	if err != nil {
		return err
	}
	for _, child := range children {
		if child.Type == metadata.InodeTypeDirectory {
			if err := deleteDirectoryRecursive(ctx, tx, child.ChildID); err != nil {
				return err
			}
			continue
		}

		file, err := tx.GetFile(ctx, store.FileSelector{InodeID: child.ChildID})
		if err != nil {
			return err
		}
		if err := deleteFileCascade(ctx, tx, file.ID); err != nil {
			return err
		}
	}
	return tx.DeleteInode(ctx, store.InodeSelector{ID: inodeID})
}

func deleteFileCascade(ctx context.Context, tx store.Tx, fileID metadata.FileID) error {
	file, err := tx.GetFile(ctx, store.FileSelector{ID: fileID})
	if err != nil {
		return err
	}

	sessions, err := tx.ListUploadSessionsByFile(ctx, file.ID, "")
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if err := tx.DeleteUploadSession(ctx, session.ID); err != nil {
			return err
		}
	}

	chunks, err := tx.ListChunksByFile(ctx, file.ID)
	if err != nil {
		return err
	}
	for _, chunk := range chunks {
		if err := tx.DeleteChunk(ctx, store.ChunkSelector{ID: chunk.ID}); err != nil {
			return err
		}
	}

	if err := tx.DeleteFile(ctx, store.FileSelector{ID: file.ID}); err != nil {
		return err
	}
	return tx.DeleteInode(ctx, store.InodeSelector{ID: file.InodeID})
}

func pathWithinPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	if prefix == "/" {
		return strings.HasPrefix(path, "/")
	}
	return strings.HasPrefix(path, strings.TrimRight(prefix, "/")+"/")
}

func replacePathPrefix(path, oldPrefix, newPrefix string) string {
	if path == oldPrefix {
		return newPrefix
	}
	trimmedOldPrefix := strings.TrimRight(oldPrefix, "/")
	if trimmedOldPrefix == "" {
		trimmedOldPrefix = "/"
	}
	if !strings.HasPrefix(path, trimmedOldPrefix+"/") {
		return path
	}
	return strings.TrimRight(newPrefix, "/") + strings.TrimPrefix(path, trimmedOldPrefix)
}
