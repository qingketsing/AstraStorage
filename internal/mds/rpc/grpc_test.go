package rpc_test

import (
	"context"
	"net"
	"testing"
	"time"

	"AstraStorage/internal/mds"
	"AstraStorage/internal/mds/grpcpb"
	"AstraStorage/internal/mds/metadata"
	"AstraStorage/internal/mds/rpc"
	"AstraStorage/internal/mds/store"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const grpcBufSize = 1024 * 1024

func TestGRPCServer_UploadLifecycle(t *testing.T) {
	client, cleanup := newGRPCClient(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	chunkVerifiedAt := now.Add(90 * time.Second)
	fileVerifiedAt := now.Add(150 * time.Second)

	if _, err := client.CreateFile(ctx, &grpcpb.CreateFileRequest{
		InodeId:   "grpc-file-inode",
		FileId:    "grpc-file",
		ParentId:  metadata.RootInodeID,
		Name:      "grpc.txt",
		Size:      64,
		CreatedAt: timestamppb.New(now),
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := client.StartUpload(ctx, &grpcpb.StartUploadRequest{
		SessionId:    "grpc-session",
		FileId:       "grpc-file",
		ExpectedSize: 64,
		CreatedAt:    timestamppb.New(now.Add(time.Minute)),
	}); err != nil {
		t.Fatalf("start upload: %v", err)
	}
	if _, err := client.CommitChunk(ctx, &grpcpb.CommitChunkRequest{
		SessionId: "grpc-session",
		ChunkId:   "grpc-chunk-0",
		Index:     0,
		Offset:    0,
		Size:      64,
		Checksum: &grpcpb.Checksum{
			Algorithm:  "sha256",
			Value:      "grpc-chunk-0",
			Verified:   true,
			VerifiedAt: timestamppb.New(chunkVerifiedAt),
		},
		Replicas: map[string]*grpcpb.ReplicaMetadata{
			"node-1": {
				NodeId: "node-1",
				Role:   string(metadata.ReplicaRolePrimary),
				State:  string(metadata.ReplicaStateReady),
			},
		},
		CommittedAt: timestamppb.New(now.Add(2 * time.Minute)),
	}); err != nil {
		t.Fatalf("commit chunk: %v", err)
	}
	if _, err := client.CompleteUpload(ctx, &grpcpb.CompleteUploadRequest{
		SessionId:        "grpc-session",
		ExpectedStatuses: []string{string(metadata.FileStatusUploading)},
		CompletedAt:      timestamppb.New(now.Add(3 * time.Minute)),
	}); err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	verifyResp, err := client.VerifyUpload(ctx, &grpcpb.VerifyUploadRequest{
		SessionId: "grpc-session",
		VerifiedChecksum: &grpcpb.Checksum{
			Algorithm:  "sha256",
			Value:      "grpc-file",
			Verified:   true,
			VerifiedAt: timestamppb.New(fileVerifiedAt),
		},
		ExpectedStatuses: []string{string(metadata.FileStatusVerifying)},
		VerifiedAt:       timestamppb.New(now.Add(4 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("verify upload: %v", err)
	}
	if verifyResp.File == nil || verifyResp.File.Status != string(metadata.FileStatusAvailable) {
		t.Fatalf("expected available file, got %#v", verifyResp.File)
	}

	planResp, err := client.BuildDownloadPlan(ctx, &grpcpb.BuildDownloadPlanRequest{FileId: "grpc-file"})
	if err != nil {
		t.Fatalf("build download plan: %v", err)
	}
	if planResp.Plan == nil || planResp.Plan.ChunkCount != 1 {
		t.Fatalf("expected one chunk in plan, got %#v", planResp.Plan)
	}
}

func TestGRPCServer_HealthAndErrorMapping(t *testing.T) {
	client, cleanup := newGRPCClient(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := client.Health(ctx, &grpcpb.HealthRequest{}); err != nil {
		t.Fatalf("health: %v", err)
	}

	now := time.Now().UTC()
	if _, err := client.CreateFile(ctx, &grpcpb.CreateFileRequest{
		InodeId:   "dup-grpc-inode",
		FileId:    "dup-grpc-file",
		ParentId:  metadata.RootInodeID,
		Name:      "dup.txt",
		Size:      32,
		CreatedAt: timestamppb.New(now),
	}); err != nil {
		t.Fatalf("first create file: %v", err)
	}
	_, err := client.CreateFile(ctx, &grpcpb.CreateFileRequest{
		InodeId:   "dup-grpc-inode-2",
		FileId:    "dup-grpc-file-2",
		ParentId:  metadata.RootInodeID,
		Name:      "dup.txt",
		Size:      32,
		CreatedAt: timestamppb.New(now),
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists, got %v", err)
	}
}

func TestGRPCServer_RegisterNode(t *testing.T) {
	client, cleanup := newGRPCClient(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	resp, err := client.RegisterNode(ctx, &grpcpb.RegisterNodeRequest{
		Id:        "grpc-node-1",
		Address:   "http://127.0.0.1:19090",
		Rack:      "rack-a",
		Zone:      "zone-a",
		Region:    "region-a",
		Labels:    map[string]string{"disk": "ssd"},
		Capacity:  2048,
		Used:      512,
		Healthy:   true,
		UpdatedAt: timestamppb.New(now),
	})
	if err != nil {
		t.Fatalf("register node: %v", err)
	}
	if resp.Node == nil {
		t.Fatalf("expected node in response")
	}
	if resp.Node.Id != "grpc-node-1" || resp.Node.Address != "http://127.0.0.1:19090" {
		t.Fatalf("unexpected node response: %#v", resp.Node)
	}
	if resp.Node.LastSeenAt == nil {
		t.Fatalf("expected last seen time to be populated")
	}
}

func TestGRPCServer_HeartbeatNodeAndAllocateUploadTargets(t *testing.T) {
	client, cleanup := newGRPCClient(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := client.RegisterNode(ctx, &grpcpb.RegisterNodeRequest{
		Id:        "grpc-alloc-node-1",
		Address:   "http://127.0.0.1:29090",
		Capacity:  2048,
		Used:      128,
		Healthy:   true,
		UpdatedAt: timestamppb.New(now),
	}); err != nil {
		t.Fatalf("register node: %v", err)
	}
	heartbeatResp, err := client.HeartbeatNode(ctx, &grpcpb.HeartbeatNodeRequest{
		NodeId:     "grpc-alloc-node-1",
		Healthy:    true,
		Capacity:   2048,
		Used:       256,
		LastSeenAt: timestamppb.New(now.Add(time.Minute)),
	})
	if err != nil {
		t.Fatalf("heartbeat node: %v", err)
	}
	if heartbeatResp.Node == nil || heartbeatResp.Node.Used != 256 {
		t.Fatalf("unexpected heartbeat response: %#v", heartbeatResp.Node)
	}
	getNodeResp, err := client.GetNode(ctx, &grpcpb.GetNodeRequest{Id: "grpc-alloc-node-1"})
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if getNodeResp.Node == nil || getNodeResp.Node.Address != "http://127.0.0.1:29090" {
		t.Fatalf("unexpected get node response: %#v", getNodeResp.Node)
	}
	if _, err := client.CreateFile(ctx, &grpcpb.CreateFileRequest{
		InodeId:   "grpc-alloc-file-inode",
		FileId:    "grpc-alloc-file",
		ParentId:  metadata.RootInodeID,
		Name:      "alloc.bin",
		Size:      16,
		CreatedAt: timestamppb.New(now),
	}); err != nil {
		t.Fatalf("create file: %v", err)
	}
	allocateResp, err := client.AllocateUploadTargets(ctx, &grpcpb.AllocateUploadTargetsRequest{
		FileId:     "grpc-alloc-file",
		ChunkIndex: 0,
	})
	if err != nil {
		t.Fatalf("allocate upload targets: %v", err)
	}
	if len(allocateResp.Targets) != 1 || allocateResp.Targets[0].NodeId != "grpc-alloc-node-1" {
		t.Fatalf("unexpected allocation response: %#v", allocateResp)
	}
}

func newGRPCClient(t *testing.T) (grpcpb.MetadataServiceClient, func()) {
	t.Helper()

	repo := store.NewMemoryRepository()
	service, err := mds.NewService(repo)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	handler, err := mds.NewHandler(service)
	if err != nil {
		t.Fatalf("new handler: %v", err)
	}
	router, err := rpc.NewRouter(handler)
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	grpcServer, err := rpc.NewGRPCServer(router, repo)
	if err != nil {
		t.Fatalf("new grpc server: %v", err)
	}
	now := time.Now().UTC()
	if err := repo.CreateInode(context.Background(), &metadata.InodeMetadata{
		ID:        metadata.InodeID(metadata.RootInodeID),
		Path:      "/",
		Type:      metadata.InodeTypeDirectory,
		Status:    metadata.InodeStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create root: %v", err)
	}

	listener := bufconn.Listen(grpcBufSize)
	go func() {
		_ = grpcServer.Serve(listener)
	}()

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithInsecure(),
	)
	if err != nil {
		t.Fatalf("dial grpc server: %v", err)
	}

	cleanup := func() {
		_ = conn.Close()
		grpcServer.Stop()
		_ = listener.Close()
	}
	return grpcpb.NewMetadataServiceClient(conn), cleanup
}
