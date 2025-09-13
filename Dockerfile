# Multi-stage Dockerfile for Multithreaded Downloader with SQLite persistence
# Stage 1: Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies for CGO and SQLite
RUN apk add --no-cache \
    gcc \
    musl-dev \
    sqlite-dev \
    git \
    ca-certificates

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o server server.go db.go

# Stage 2: Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    sqlite \
    tzdata

# Create app user for security
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup

# Create directories
RUN mkdir -p /app/data /app/downloads && \
    chown -R appuser:appgroup /app

# Set working directory
WORKDIR /app

# Copy binary from builder stage
COPY --from=builder /app/server .
COPY --chown=appuser:appgroup --from=builder /app/server .

# Copy additional files if needed (optional)
COPY --chown=appuser:appgroup README.md PERSISTENCE_README.md ./

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Set environment variables
ENV GIN_MODE=release
ENV DATABASE_PATH=/app/data/downloads.db

# Create volume for persistent data
VOLUME ["/app/data", "/app/downloads"]

# Run the application
CMD ["./server"]
