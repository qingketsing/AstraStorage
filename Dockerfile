# Multi-stage build for smaller image
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/node ./cmd/node

# Final stage - minimal runtime image
FROM alpine:latest

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates

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
EXPOSE 29001 9081

# Set working directory
WORKDIR /data

# Run the node
ENTRYPOINT ["/bin/node"]
