package logging

import (
	"context"
	"net/http"
)

const RequestIDHeader = "X-Request-ID"

type requestIDKey struct{}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func SetRequestIDHeader(header http.Header, requestID string) {
	if header == nil {
		return
	}
	if requestID == "" {
		header.Del(RequestIDHeader)
		return
	}
	header.Set(RequestIDHeader, requestID)
}

func RequestIDFromHeader(header http.Header) string {
	if header == nil {
		return ""
	}
	return header.Get(RequestIDHeader)
}
