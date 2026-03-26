package rpc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"AstraStorage/internal/mds/grpcpb"
	"AstraStorage/internal/mds/store"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type grpcService struct {
	grpcpb.UnimplementedMetadataServiceServer
	router *Router
	health store.HealthChecker
}

// NewGRPCServer 基于现有 Router 和 HealthChecker 构建 gRPC Server。
func NewGRPCServer(router *Router, health store.HealthChecker) (*grpc.Server, error) {
	service, err := NewGRPCService(router, health)
	if err != nil {
		return nil, err
	}
	server := grpc.NewServer()
	grpcpb.RegisterMetadataServiceServer(server, service)
	return server, nil
}

// NewGRPCService 返回可注册到 gRPC Server 的 MetadataService 实现。
func NewGRPCService(router *Router, health store.HealthChecker) (grpcpb.MetadataServiceServer, error) {
	if router == nil {
		return nil, errors.New("mds/rpc/grpc: router is nil")
	}
	if health == nil {
		return nil, errors.New("mds/rpc/grpc: health checker is nil")
	}
	return &grpcService{
		router: router,
		health: health,
	}, nil
}

func (s *grpcService) Health(ctx context.Context, _ *grpcpb.HealthRequest) (*grpcpb.HealthResponse, error) {
	if err := s.health.Ping(ctx); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &grpcpb.HealthResponse{Status: "ok"}, nil
}

func (s *grpcService) CreateDirectory(ctx context.Context, req *grpcpb.CreateDirectoryRequest) (*grpcpb.CreateDirectoryResponse, error) {
	return bridgeUnary(ctx, s.router, MethodCreateDirectory, req, func() *grpcpb.CreateDirectoryResponse { return &grpcpb.CreateDirectoryResponse{} })
}

func (s *grpcService) CreateFile(ctx context.Context, req *grpcpb.CreateFileRequest) (*grpcpb.CreateFileResponse, error) {
	return bridgeUnary(ctx, s.router, MethodCreateFile, req, func() *grpcpb.CreateFileResponse { return &grpcpb.CreateFileResponse{} })
}

func (s *grpcService) RegisterNode(ctx context.Context, req *grpcpb.RegisterNodeRequest) (*grpcpb.RegisterNodeResponse, error) {
	return bridgeUnary(ctx, s.router, MethodRegisterNode, req, func() *grpcpb.RegisterNodeResponse { return &grpcpb.RegisterNodeResponse{} })
}

func (s *grpcService) HeartbeatNode(ctx context.Context, req *grpcpb.HeartbeatNodeRequest) (*grpcpb.HeartbeatNodeResponse, error) {
	return bridgeUnary(ctx, s.router, MethodHeartbeatNode, req, func() *grpcpb.HeartbeatNodeResponse { return &grpcpb.HeartbeatNodeResponse{} })
}

func (s *grpcService) AllocateUploadTargets(ctx context.Context, req *grpcpb.AllocateUploadTargetsRequest) (*grpcpb.AllocateUploadTargetsResponse, error) {
	return bridgeUnary(ctx, s.router, MethodAllocateUploadTargets, req, func() *grpcpb.AllocateUploadTargetsResponse {
		return &grpcpb.AllocateUploadTargetsResponse{}
	})
}

func (s *grpcService) StartUpload(ctx context.Context, req *grpcpb.StartUploadRequest) (*grpcpb.StartUploadResponse, error) {
	return bridgeUnary(ctx, s.router, MethodStartUpload, req, func() *grpcpb.StartUploadResponse { return &grpcpb.StartUploadResponse{} })
}

func (s *grpcService) CommitChunk(ctx context.Context, req *grpcpb.CommitChunkRequest) (*grpcpb.CommitChunkResponse, error) {
	return bridgeUnary(ctx, s.router, MethodCommitChunk, req, func() *grpcpb.CommitChunkResponse { return &grpcpb.CommitChunkResponse{} })
}

func (s *grpcService) CompleteUpload(ctx context.Context, req *grpcpb.CompleteUploadRequest) (*grpcpb.CompleteUploadResponse, error) {
	return bridgeUnary(ctx, s.router, MethodCompleteUpload, req, func() *grpcpb.CompleteUploadResponse { return &grpcpb.CompleteUploadResponse{} })
}

func (s *grpcService) VerifyUpload(ctx context.Context, req *grpcpb.VerifyUploadRequest) (*grpcpb.VerifyUploadResponse, error) {
	return bridgeUnary(ctx, s.router, MethodVerifyUpload, req, func() *grpcpb.VerifyUploadResponse { return &grpcpb.VerifyUploadResponse{} })
}

func (s *grpcService) FailUploadVerification(ctx context.Context, req *grpcpb.FailUploadVerificationRequest) (*grpcpb.FailUploadVerificationResponse, error) {
	return bridgeUnary(ctx, s.router, MethodFailUploadVerification, req, func() *grpcpb.FailUploadVerificationResponse {
		return &grpcpb.FailUploadVerificationResponse{}
	})
}

func (s *grpcService) RetryUpload(ctx context.Context, req *grpcpb.RetryUploadRequest) (*grpcpb.RetryUploadResponse, error) {
	return bridgeUnary(ctx, s.router, MethodRetryUpload, req, func() *grpcpb.RetryUploadResponse { return &grpcpb.RetryUploadResponse{} })
}

func (s *grpcService) RenameInode(ctx context.Context, req *grpcpb.RenameInodeRequest) (*grpcpb.RenameInodeResponse, error) {
	return bridgeUnary(ctx, s.router, MethodRenameInode, req, func() *grpcpb.RenameInodeResponse { return &grpcpb.RenameInodeResponse{} })
}

func (s *grpcService) MoveInode(ctx context.Context, req *grpcpb.MoveInodeRequest) (*grpcpb.MoveInodeResponse, error) {
	return bridgeUnary(ctx, s.router, MethodMoveInode, req, func() *grpcpb.MoveInodeResponse { return &grpcpb.MoveInodeResponse{} })
}

func (s *grpcService) DeleteFile(ctx context.Context, req *grpcpb.DeleteFileRequest) (*grpcpb.DeleteFileResponse, error) {
	return bridgeUnary(ctx, s.router, MethodDeleteFile, req, func() *grpcpb.DeleteFileResponse { return &grpcpb.DeleteFileResponse{} })
}

func (s *grpcService) DeleteDirectory(ctx context.Context, req *grpcpb.DeleteDirectoryRequest) (*grpcpb.DeleteDirectoryResponse, error) {
	return bridgeUnary(ctx, s.router, MethodDeleteDirectory, req, func() *grpcpb.DeleteDirectoryResponse {
		return &grpcpb.DeleteDirectoryResponse{}
	})
}

func (s *grpcService) GetInode(ctx context.Context, req *grpcpb.GetInodeRequest) (*grpcpb.GetInodeResponse, error) {
	return bridgeUnary(ctx, s.router, MethodGetInode, req, func() *grpcpb.GetInodeResponse { return &grpcpb.GetInodeResponse{} })
}

func (s *grpcService) GetFile(ctx context.Context, req *grpcpb.GetFileRequest) (*grpcpb.GetFileResponse, error) {
	return bridgeUnary(ctx, s.router, MethodGetFile, req, func() *grpcpb.GetFileResponse { return &grpcpb.GetFileResponse{} })
}

func (s *grpcService) GetNode(ctx context.Context, req *grpcpb.GetNodeRequest) (*grpcpb.GetNodeResponse, error) {
	return bridgeUnary(ctx, s.router, MethodGetNode, req, func() *grpcpb.GetNodeResponse { return &grpcpb.GetNodeResponse{} })
}

func (s *grpcService) ListChildren(ctx context.Context, req *grpcpb.ListChildrenRequest) (*grpcpb.ListChildrenResponse, error) {
	return bridgeUnary(ctx, s.router, MethodListChildren, req, func() *grpcpb.ListChildrenResponse { return &grpcpb.ListChildrenResponse{} })
}

func (s *grpcService) ListFileChunks(ctx context.Context, req *grpcpb.ListFileChunksRequest) (*grpcpb.ListFileChunksResponse, error) {
	return bridgeUnary(ctx, s.router, MethodListFileChunks, req, func() *grpcpb.ListFileChunksResponse { return &grpcpb.ListFileChunksResponse{} })
}

func (s *grpcService) GetUploadSession(ctx context.Context, req *grpcpb.GetUploadSessionRequest) (*grpcpb.GetUploadSessionResponse, error) {
	return bridgeUnary(ctx, s.router, MethodGetUploadSession, req, func() *grpcpb.GetUploadSessionResponse {
		return &grpcpb.GetUploadSessionResponse{}
	})
}

func (s *grpcService) BuildDownloadPlan(ctx context.Context, req *grpcpb.BuildDownloadPlanRequest) (*grpcpb.BuildDownloadPlanResponse, error) {
	return bridgeUnary(ctx, s.router, MethodBuildDownloadPlan, req, func() *grpcpb.BuildDownloadPlanResponse {
		return &grpcpb.BuildDownloadPlanResponse{}
	})
}

func bridgeUnary[Req proto.Message, Resp proto.Message](ctx context.Context, router *Router, method string, grpcReq Req, newResp func() Resp) (Resp, error) {
	var zero Resp

	rpcReq, err := newRequestPayload(method)
	if err != nil {
		return zero, status.Error(codes.Unimplemented, err.Error())
	}
	if err := assignValue(reflect.ValueOf(rpcReq), reflect.ValueOf(grpcReq)); err != nil {
		return zero, status.Error(codes.Internal, fmt.Sprintf("map grpc request: %v", err))
	}

	value := reflect.ValueOf(rpcReq)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return zero, status.Error(codes.Internal, fmt.Sprintf("rpc request payload for %s is invalid", method))
	}

	rpcResp, err := router.Dispatch(ctx, method, value.Elem().Interface())
	if err != nil {
		return zero, mapGRPCError(err)
	}

	resp := newResp()
	if err := assignValue(reflect.ValueOf(resp), reflect.ValueOf(rpcResp)); err != nil {
		return zero, status.Error(codes.Internal, fmt.Sprintf("map grpc response: %v", err))
	}
	return resp, nil
}

func mapGRPCError(err error) error {
	switch {
	case errors.Is(err, store.ErrInvalidArgument):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, store.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, store.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, store.ErrConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

func assignValue(dst, src reflect.Value) error {
	if !src.IsValid() {
		return nil
	}
	if src.Kind() == reflect.Pointer {
		if src.IsNil() {
			return nil
		}
		return assignValue(dst, src.Elem())
	}
	if dst.Kind() == reflect.Pointer {
		if dst.IsNil() {
			dst.Set(reflect.New(dst.Type().Elem()))
		}
		return assignValue(dst.Elem(), src)
	}

	if isTimeType(dst.Type()) && isTimestampType(src.Type()) {
		timestamp := src.Interface().(timestamppb.Timestamp)
		dst.Set(reflect.ValueOf(timestamp.AsTime()))
		return nil
	}
	if isTimestampType(dst.Type()) && isTimeType(src.Type()) {
		t := src.Interface().(time.Time)
		dst.Set(reflect.ValueOf(*timestamppb.New(t)))
		return nil
	}

	switch dst.Kind() {
	case reflect.Struct:
		if src.Kind() != reflect.Struct {
			return assignSimpleValue(dst, src)
		}
		return assignStruct(dst, src)
	case reflect.Map:
		if src.Kind() != reflect.Map {
			return assignSimpleValue(dst, src)
		}
		return assignMap(dst, src)
	case reflect.Slice:
		if src.Kind() != reflect.Slice {
			return assignSimpleValue(dst, src)
		}
		return assignSlice(dst, src)
	default:
		return assignSimpleValue(dst, src)
	}
}

func assignStruct(dst, src reflect.Value) error {
	srcFields := make(map[string]reflect.Value, src.NumField())
	for i := 0; i < src.NumField(); i++ {
		field := src.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		srcFields[normalizeFieldName(field.Name)] = src.Field(i)
	}

	for i := 0; i < dst.NumField(); i++ {
		field := dst.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		srcField, ok := srcFields[normalizeFieldName(field.Name)]
		if !ok {
			continue
		}
		if err := assignValue(dst.Field(i), srcField); err != nil {
			return fmt.Errorf("assign field %s: %w", field.Name, err)
		}
	}
	return nil
}

func assignMap(dst, src reflect.Value) error {
	if dst.IsNil() {
		dst.Set(reflect.MakeMapWithSize(dst.Type(), src.Len()))
	}
	iter := src.MapRange()
	for iter.Next() {
		key := reflect.New(dst.Type().Key()).Elem()
		if err := assignValue(key, iter.Key()); err != nil {
			return fmt.Errorf("assign map key: %w", err)
		}
		value := reflect.New(dst.Type().Elem()).Elem()
		if err := assignValue(value, iter.Value()); err != nil {
			return fmt.Errorf("assign map value for %v: %w", iter.Key(), err)
		}
		dst.SetMapIndex(key, value)
	}
	return nil
}

func assignSlice(dst, src reflect.Value) error {
	slice := reflect.MakeSlice(dst.Type(), src.Len(), src.Len())
	for i := 0; i < src.Len(); i++ {
		if err := assignValue(slice.Index(i), src.Index(i)); err != nil {
			return fmt.Errorf("assign slice index %d: %w", i, err)
		}
	}
	dst.Set(slice)
	return nil
}

func assignSimpleValue(dst, src reflect.Value) error {
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return nil
	}
	if src.Type().ConvertibleTo(dst.Type()) {
		dst.Set(src.Convert(dst.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %s to %s", src.Type(), dst.Type())
}

func normalizeFieldName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "_", "")
	name = strings.ReplaceAll(name, "id", "id")
	return name
}

func isTimeType(t reflect.Type) bool {
	return t == reflect.TypeOf(time.Time{})
}

func isTimestampType(t reflect.Type) bool {
	return t == reflect.TypeOf(timestamppb.Timestamp{})
}
