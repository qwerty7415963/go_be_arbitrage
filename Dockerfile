# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o /arbitrage-be .

# Runtime stage
FROM alpine:3.19

# Security: install only what's needed
RUN apk add --no-cache ca-certificates tzdata curl && \
    rm -rf /var/cache/apk/*

# Security: create non-root user with specific UID/GID
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup -h /app -s /sbin/nologin

# Security: create required directories with proper ownership
RUN mkdir -p /app/data /app/logs /app/config && \
    chown -R appuser:appgroup /app

WORKDIR /app

# Copy binary from builder
COPY --from=builder --chown=appuser:appgroup /arbitrage-be .
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Security: switch to non-root user
USER appuser

EXPOSE 8080

# Security: use non-root healthcheck
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -sf http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/arbitrage-be"]
