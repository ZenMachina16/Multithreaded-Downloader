# Database Persistence Implementation

## Overview

The multithreaded downloader now includes SQLite database persistence using GORM. This provides:

- **Persistent download tracking** - Downloads survive server restarts
- **Automatic resume** - Incomplete downloads resume automatically on server startup
- **Progress tracking** - Real-time progress updates saved every 3 seconds
- **Download statistics** - Historical data and analytics
- **Cleanup automation** - Old completed downloads are automatically cleaned up

## Database Schema

### Download Table

```sql
CREATE TABLE downloads (
    id TEXT PRIMARY KEY,              -- UUID download identifier
    url TEXT NOT NULL,                -- Source URL
    output_path TEXT NOT NULL,        -- Local file path
    threads INTEGER NOT NULL DEFAULT 4, -- Number of threads used
    status TEXT NOT NULL DEFAULT 'downloading', -- Current status
    bytes_downloaded INTEGER DEFAULT 0, -- Bytes downloaded so far
    total_bytes INTEGER DEFAULT 0,    -- Total file size
    start_time DATETIME NOT NULL,     -- When download started
    updated_at DATETIME,              -- Last update timestamp
    created_at DATETIME,              -- Record creation time
    error TEXT                        -- Error message (if failed)
);
```

### Status Values

- `downloading` - Download is actively running
- `paused` - Download is temporarily paused
- `completed` - Download finished successfully
- `failed` - Download failed with an error

## New Features

### 1. Automatic Resume on Restart

When the server starts, it automatically:
1. Queries the database for downloads with status `downloading` or `paused`
2. Recreates the downloader instances
3. Resumes downloads from where they left off
4. Updates progress in real-time

### 2. Persistent Progress Tracking

- Progress is saved to database every 3 seconds
- Includes bytes downloaded, total size, and current status
- Survives server crashes and restarts

### 3. New API Endpoint: Statistics

**GET /stats** - Returns download statistics:

```json
{
  "statistics": {
    "total": 15,
    "downloading": 2,
    "paused": 1,
    "completed": 10,
    "failed": 2
  },
  "timestamp": "2023-12-07T10:30:00Z"
}
```

### 4. Automatic Cleanup

- Completed downloads older than 7 days are automatically removed
- Cleanup runs daily at server startup + 24-hour intervals
- Configurable retention period

## File Structure

```
multithreaded-downloader/
├── main.go              # Original CLI tool
├── server.go            # REST API server with persistence
├── db.go                # Database models and operations
├── downloader/          # Core download logic
├── downloads.db         # SQLite database (created automatically)
├── test_persistence.go  # Database test script
└── PERSISTENCE_README.md # This documentation
```

## API Changes

All existing endpoints remain the same, with enhanced functionality:

### Enhanced Responses

**GET /downloads/:id/status** now includes database fields:

```json
{
  "download_id": "uuid-here",
  "url": "https://example.com/file.zip",
  "filename": "file.zip",
  "status": "downloading",
  "percent_completed": 45.2,
  "bytes_downloaded": 4521984,
  "total_size": 10000000,
  "threads_used": 4,
  "start_time": "2023-12-07T10:00:00Z"
}
```

### New Endpoints

- **GET /stats** - Download statistics
- **GET /api/v1/stats** - Same as above with API versioning

## Installation & Setup

### Prerequisites

- Go 1.19+ with CGO support
- GCC compiler (for SQLite)

### Quick Start

```bash
# 1. Install dependencies
go mod tidy

# 2. Run the server
go run server.go db.go

# 3. Test persistence (optional)
go run test_persistence.go db.go
```

### CGO Issues on Windows

If you encounter CGO compilation errors on Windows:

1. **Install TDM-GCC or MinGW-w64**
2. **Use WSL (Windows Subsystem for Linux)**
3. **Use Docker** (see Docker section below)

### Docker Alternative

```dockerfile
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY . .
RUN go mod tidy && go build -o server server.go db.go

FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /root/
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
```

## Configuration

### Database Location

Default: `downloads.db` in the current directory

To change, modify the `InitDatabase` call in `server.go`:

```go
// Custom database path
if err := InitDatabase("/path/to/custom/downloads.db"); err != nil {
    log.Fatalf("Failed to initialize database: %v", err)
}
```

### Cleanup Schedule

Default: Remove completed downloads older than 7 days

To change, modify the cleanup goroutine in `server.go`:

```go
// Custom retention period (30 days)
if err := dbManager.CleanupCompletedDownloads(30 * 24 * time.Hour); err != nil {
    fmt.Printf("Error during cleanup: %v\n", err)
}
```

### Progress Update Frequency

Default: Every 3 seconds

To change, modify the ticker in the download goroutines:

```go
// Update every 5 seconds instead
progressTicker := time.NewTicker(5 * time.Second)
```

## Database Operations

### Manual Database Access

You can inspect the database directly:

```bash
# Install SQLite CLI
# Ubuntu/Debian: sudo apt install sqlite3
# macOS: brew install sqlite3
# Windows: Download from sqlite.org

# Open database
sqlite3 downloads.db

# View all downloads
.headers on
.mode table
SELECT * FROM downloads;

# View only active downloads
SELECT id, url, status, bytes_downloaded, total_bytes 
FROM downloads 
WHERE status IN ('downloading', 'paused');
```

### Backup & Recovery

```bash
# Backup database
sqlite3 downloads.db ".backup downloads_backup.db"

# Restore from backup
cp downloads_backup.db downloads.db
```

## Performance Considerations

### Database Performance

- SQLite handles concurrent reads well
- Write operations are serialized (not a bottleneck for this use case)
- Database file grows ~200 bytes per download record
- Automatic cleanup prevents unlimited growth

### Memory Usage

- Each active download: ~50KB in memory
- Database operations: Minimal overhead
- Progress updates: Batched every 3 seconds

### Disk Usage

- Database file: ~200 bytes per download
- Log files: Configurable via GORM logger
- Downloaded files: As configured by user

## Troubleshooting

### Common Issues

1. **CGO Compilation Errors**
   - Install proper C compiler
   - Use Docker or WSL on Windows
   - Consider pure Go SQLite alternatives

2. **Database Lock Errors**
   - Ensure only one server instance is running
   - Check file permissions on database file
   - Verify disk space availability

3. **Resume Not Working**
   - Check that progress files exist in working directory
   - Verify database contains incomplete downloads
   - Ensure URLs are still accessible

### Debug Mode

Enable GORM debug logging in `db.go`:

```go
// Change LogLevel to Info or Warn for more verbose logging
LogLevel: logger.Info,  // Instead of logger.Error
```

### Health Checks

The server provides several health endpoints:

```bash
# Basic health check
curl http://localhost:8080/health

# Download statistics
curl http://localhost:8080/stats

# List all downloads
curl http://localhost:8080/downloads
```

## Migration from Non-Persistent Version

If you have an existing server without persistence:

1. **Backup existing downloads** (if any are in progress)
2. **Stop the old server**
3. **Deploy new version** with database support
4. **Restart server** - database will be created automatically
5. **Resume any interrupted downloads** manually via API

The new version is fully backward compatible with the original API.

## Contributing

When adding new database features:

1. **Update the Download model** in `db.go`
2. **Add migration logic** if needed
3. **Update API responses** to include new fields
4. **Add tests** in `test_persistence.go`
5. **Update this documentation**

## License

Same as the main project.
