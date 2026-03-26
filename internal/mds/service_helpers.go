package mds

import (
	"context"
	"strings"
	"time"

	"AstraStorage/internal/mds/metadata"
)

func requestTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}

func childPath(parentPath, name string) string {
	if parentPath == "" || parentPath == "/" {
		return "/" + name
	}
	return strings.TrimRight(parentPath, "/") + "/" + name
}

func cloneStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func cloneTimePtr(src *time.Time) *time.Time {
	if src == nil {
		return nil
	}
	t := *src
	return &t
}

func cloneChecksumPtr(src *metadata.Checksum) *metadata.Checksum {
	if src == nil {
		return nil
	}
	checksum := *src
	if src.VerifiedAt != nil {
		t := *src.VerifiedAt
		checksum.VerifiedAt = &t
	}
	return &checksum
}

func cloneInode(src *metadata.InodeMetadata) *metadata.InodeMetadata {
	if src == nil {
		return nil
	}
	dst := *src
	if src.AccessedAt != nil {
		t := *src.AccessedAt
		dst.AccessedAt = &t
	}
	return &dst
}

func cloneFile(src *metadata.FileMetadata) *metadata.FileMetadata {
	if src == nil {
		return nil
	}
	dst := *src
	dst.SecondaryNodeIDs = append([]metadata.NodeID(nil), src.SecondaryNodeIDs...)
	dst.UserMetadata = cloneStringMap(src.UserMetadata)
	dst.Tags = cloneStringMap(src.Tags)
	if src.CompletedAt != nil {
		t := *src.CompletedAt
		dst.CompletedAt = &t
	}
	if src.NodePlacements != nil {
		dst.NodePlacements = make(metadata.NodePlacements, len(src.NodePlacements))
		for nodeID, placement := range src.NodePlacements {
			dst.NodePlacements[nodeID] = placement
		}
	}
	return &dst
}

func cloneUploadSession(src *metadata.UploadSession) *metadata.UploadSession {
	if src == nil {
		return nil
	}
	dst := *src
	dst.ExpectedChecksum = cloneChecksumPtr(src.ExpectedChecksum)
	dst.VerifiedChecksum = cloneChecksumPtr(src.VerifiedChecksum)
	dst.ClientMetadata = cloneStringMap(src.ClientMetadata)
	dst.TransportAttributes = cloneStringMap(src.TransportAttributes)
	if src.ExpiresAt != nil {
		t := *src.ExpiresAt
		dst.ExpiresAt = &t
	}
	if src.CompletedAt != nil {
		t := *src.CompletedAt
		dst.CompletedAt = &t
	}
	if src.Retry.LastFailureAt != nil {
		t := *src.Retry.LastFailureAt
		dst.Retry.LastFailureAt = &t
	}
	if src.Retry.NextRetryAt != nil {
		t := *src.Retry.NextRetryAt
		dst.Retry.NextRetryAt = &t
	}
	return &dst
}

func cloneNode(src *metadata.NodeInfo) *metadata.NodeInfo {
	if src == nil {
		return nil
	}
	dst := *src
	dst.Labels = cloneStringMap(src.Labels)
	if src.LastSeenAt != nil {
		t := *src.LastSeenAt
		dst.LastSeenAt = &t
	}
	return &dst
}

func cloneNodes(src []metadata.NodeInfo) []metadata.NodeInfo {
	if src == nil {
		return nil
	}
	dst := make([]metadata.NodeInfo, len(src))
	for i, node := range src {
		dst[i] = *cloneNode(&node)
	}
	return dst
}

func int64Ptr(v int64) *int64 {
	return &v
}

func uploadSessionIDPtr(v metadata.UploadSessionID) *metadata.UploadSessionID {
	return &v
}

func cloneDownloadPlan(src *DownloadPlan) *DownloadPlan {
	if src == nil {
		return nil
	}
	dst := *src
	if src.Chunks != nil {
		dst.Chunks = make([]DownloadChunkPlan, len(src.Chunks))
		for i, chunk := range src.Chunks {
			dst.Chunks[i] = chunk
			dst.Chunks[i].CandidateNodeIDs = append([]metadata.NodeID(nil), chunk.CandidateNodeIDs...)
		}
	}
	return &dst
}

func cloneDirectoryEntries(src []metadata.DirectoryEntry) []metadata.DirectoryEntry {
	if src == nil {
		return nil
	}
	dst := make([]metadata.DirectoryEntry, len(src))
	copy(dst, src)
	return dst
}

func (s *Service) invalidateFileReadModels(ctx context.Context, fileID metadata.FileID) {
	if s == nil || s.readCache == nil || fileID == "" {
		return
	}
	_ = s.readCache.InvalidateFile(ctx, fileID)
}

func (s *Service) invalidateDirectoryReadModels(ctx context.Context, inodeID metadata.InodeID) {
	if s == nil || s.readCache == nil || inodeID == "" {
		return
	}
	_ = s.readCache.InvalidateDirectory(ctx, inodeID)
}

func (s *Service) invalidateNodeReadModels(ctx context.Context, nodeID metadata.NodeID) {
	if s == nil || s.readCache == nil || nodeID == "" {
		return
	}
	_ = s.readCache.InvalidateNode(ctx, nodeID)
}

func (s *Service) invalidateHealthyNodeReadModels(ctx context.Context) {
	if s == nil || s.readCache == nil {
		return
	}
	_ = s.readCache.InvalidateHealthyNodes(ctx)
}
