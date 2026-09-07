# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git make

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o apiserver ./cmd/apiserver

# Runtime stage
FROM alpine:3.19

WORKDIR /app

# Install minimal runtime dependencies
RUN apk add --no-cache ca-certificates tzdata && \
    mkdir -p /etc/cloudai-fusion && \
    chmod 755 /etc/cloudai-fusion

# Copy binary from builder
COPY --from=builder /app/apiserver /app/apiserver

# Create non-root user for security
RUN adduser -D -u 1000 cloudai && \
    chown -R cloudai:cloudai /app

USER cloudai

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/apiserver"]
CMD ["--config", "/etc/cloudai-fusion/config.yaml"]
