# Multi-stage build for smaller image
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Install necessary build tools
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
ENV GOPROXY=https://goproxy.cn,direct
ENV CGO_ENABLED=0
ENV GOOS=linux
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN go build -ldflags="-w -s" -o /bin/node ./cmd/node

# Final stage - minimal runtime image
FROM alpine:latest

# Install ca-certificates for HTTPS and netcat for debugging
RUN apk --no-cache add ca-certificates netcat-openbsd

# Create non-root user
RUN addgroup -g 1000 nodeuser && \
    adduser -D -u 1000 -G nodeuser nodeuser

# Create data directory
RUN mkdir -p /data && chown nodeuser:nodeuser /data

# Copy binary from builder
COPY --from=builder /bin/node /bin/node
RUN chmod +x /bin/node

# Switch to non-root user
USER nodeuser

# Expose ports
# 29001-29005: Raft RPC ports
# 9081-9085: Health check HTTP ports
# 30001-30005: Upload ports
# 31001-31005: Download ports
EXPOSE 29001 9081 30001 31001

# Set working directory
WORKDIR /data

# Run the node
ENTRYPOINT ["/bin/node"]
