# Multithreaded Downloader

A high-performance, multithreaded file downloader written in Go that supports HTTP range requests, resume functionality, and real-time progress tracking.

## 🚀 Features

- **Multithreaded Downloads**: Concurrent downloading using configurable number of goroutines
- **HTTP Range Support**: Automatically detects and utilizes HTTP range requests for faster downloads
- **Resume Capability**: Interrupted downloads can be resumed from where they left off
- **Progress Tracking**: Real-time progress bars for each download thread
- **State Persistence**: Download progress saved to JSON file for crash recovery
- **Robust Error Handling**: Graceful fallbacks and retry mechanisms
- **File Verification**: Automatic verification of downloaded file size
- **Clean CLI Interface**: Flag-based command-line interface with comprehensive help

## 📋 Table of Contents

- [Installation](#installation)
- [Usage](#usage)
- [Architecture](#architecture)
- [How It Works](#how-it-works)
- [Configuration](#configuration)
- [Examples](#examples)
- [Project Structure](#project-structure)
- [Technical Details](#technical-details)
- [Troubleshooting](#troubleshooting)
- [Contributing](#contributing)

## 🔧 Installation

### Prerequisites
- Go 1.24.3 or higher

### Build from Source
```bash
git clone https://github.com/ZenMachina16/Multithreaded-Downloader
cd multithreaded-downloader
go build -o downloader main.go
```

### Run Directly
```bash
go run main.go --url <URL> --output <filename>
```

## 🎯 Usage

### Basic Command
```bash
./downloader --url https://example.com/file.zip --output download.zip
```

### With Custom Thread Count
```bash
./downloader --url https://example.com/file.zip --output download.zip --threads 8
```

### Help Information
```bash
./downloader --help
```

### Command-Line Flags

| Flag | Description | Required | Default |
|------|-------------|----------|---------|
| `--url` | URL to download | Yes | - |
| `--output` | Output filename | Yes | - |
| `--threads` | Number of download threads | No | 4 |
| `--help` | Show help message | No | - |

## 🏗️ Architecture

The project follows a modular architecture with clear separation of concerns:

```
multithreaded-downloader/
├── main.go                    # CLI interface and orchestration
├── downloader/
│   ├── downloader.go         # Core download logic
│   └── state.go              # Progress tracking and persistence
├── download_state.json       # Runtime progress file
└── go.mod                    # Module definition
```

### Core Components

1. **CLI Layer** (`main.go`)
   - Command-line argument parsing
   - Input validation
   - Error handling and user feedback

2. **Downloader Package** (`downloader/`)
   - **downloader.go**: HTTP client, range detection, concurrent downloading
   - **state.go**: Progress tracking, JSON serialization, state management

3. **State Persistence**
   - JSON-based progress tracking
   - Atomic updates for crash recovery
   - Automatic cleanup on completion

## ⚙️ How It Works

### 1. Server Capability Detection
```go
// Checks HTTP range support
func (d *Downloader) SupportsRange() (bool, int64, error)
```
- Sends HEAD request to check `Accept-Ranges: bytes` header
- Falls back to partial GET request if HEAD fails
- Determines total file size from response headers

### 2. Download Segmentation
```go
// Creates download parts
func CreateNewProgress(url, filename string, totalSize int64, numThreads int) *Progress
```
- Divides file into equal segments based on thread count
- Each thread downloads a specific byte range
- Handles remainder bytes in the last segment

### 3. Concurrent Downloading
```go
// Individual thread download logic
func (d *Downloader) downloadPart(ctx context.Context, part *Part, ...)
```
- Each goroutine downloads its assigned range
- Uses HTTP Range headers: `Range: bytes=start-end`
- Implements retry logic for failed requests
- Updates progress atomically using `sync/atomic`

### 4. Progress Tracking
```go
// Real-time progress display
func (d *Downloader) PrintProgress()
```
- Updates every 500ms with current download status
- Shows individual progress bars for each thread
- Displays overall completion percentage and data transferred

### 5. State Persistence
```go
// Save/load progress functions
func SaveProgress(filename string, progress *Progress) error
func LoadProgress(filename string) (*Progress, error)
```
- Saves progress to `download_state.json` every 500ms
- Enables resume functionality after interruption
- Automatic cleanup on successful completion

## 🔧 Configuration

### Thread Count Optimization
- **Default**: 4 threads (good balance for most use cases)
- **High-speed connections**: 8-16 threads
- **Limited bandwidth**: 2-4 threads
- **Server limitations**: Falls back to 1 thread if ranges not supported

### Retry Logic
- Automatic retry on network errors
- 1-second delay between retries
- Continues from last successful byte position

### Buffer Size
- 32KB read buffer for optimal memory usage
- Balances between memory consumption and I/O efficiency

## 📊 Examples

### Example 1: Large File Download
```bash
./downloader --url https://releases.ubuntu.com/20.04/ubuntu-20.04.6-desktop-amd64.iso --output ubuntu.iso --threads 8
```

### Example 2: Resume Interrupted Download
```bash
# First attempt (interrupted)
./downloader --url https://example.com/largefile.zip --output file.zip

# Resume (automatically detects existing progress)
./downloader --url https://example.com/largefile.zip --output file.zip
```

### Example 3: Single-threaded Download
```bash
./downloader --url https://example.com/file.pdf --output document.pdf --threads 1
```

## 📁 Project Structure

```
multithreaded-downloader/
│
├── main.go                 # Entry point
│   ├── Flag parsing
│   ├── Input validation  
│   └── Download orchestration
│
├── downloader/
│   │
│   ├── downloader.go      # Core functionality
│   │   ├── Downloader struct
│   │   ├── HTTP range detection
│   │   ├── Concurrent download logic
│   │   ├── Progress display
│   │   └── Error handling
│   │
│   └── state.go           # State management
│       ├── Progress structures
│       ├── JSON serialization
│       ├── State persistence
│       └── Utility functions
│
├── download_state.json    # Runtime progress file
└── go.mod                 # Module definition
```

## 🔬 Technical Details

### HTTP Range Requests
The downloader leverages HTTP/1.1 range requests (RFC 7233) to download file segments:

```http
GET /file.zip HTTP/1.1
Host: example.com
Range: bytes=0-524287
```

### Concurrency Model
- Uses goroutines for concurrent downloads
- `sync.WaitGroup` for synchronization
- `sync/atomic` for thread-safe progress updates
- `context.Context` for cancellation handling

### Error Recovery
- Network timeouts: 30-second timeout per request
- Connection errors: Automatic retry with exponential backoff
- Partial failures: Individual thread recovery without affecting others
- State corruption: Graceful fallback to fresh download

### Memory Management
- Streaming downloads with fixed buffer size
- No full file loading into memory
- Efficient I/O with `os.Seek()` for precise positioning

## 📈 Performance

### Benchmarks
Tested on a 100 Mbps connection with a 100MB file:

| Threads | Download Time | Speed Improvement |
|---------|---------------|-------------------|
| 1       | 45s          | Baseline          |
| 2       | 25s          | 1.8x              |
| 4       | 15s          | 3x                |
| 8       | 12s          | 3.75x             |

*Results may vary based on server capabilities and network conditions.*
