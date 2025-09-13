package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"multithreaded-downloader/downloader"
)

// Simple download request
type SimpleDownloadRequest struct {
	URL     string `json:"url"`
	Output  string `json:"output"`
	Threads int    `json:"threads"`
}

// Simple download response
type SimpleDownloadResponse struct {
	DownloadID string `json:"download_id"`
	Message    string `json:"message"`
}

// Simple download status
type SimpleDownloadStatus struct {
	DownloadID       string  `json:"download_id"`
	URL              string  `json:"url"`
	Filename         string  `json:"filename"`
	Status           string  `json:"status"`
	PercentCompleted float64 `json:"percent_completed"`
	BytesDownloaded  int64   `json:"bytes_downloaded"`
	TotalSize        int64   `json:"total_size"`
	ThreadsUsed      int     `json:"threads_used"`
	StartTime        string  `json:"start_time"`
	Error            string  `json:"error,omitempty"`
}

// Simple managed download
type SimpleManagedDownload struct {
	ID         string
	Downloader *downloader.Downloader
	Status     string
	StartTime  time.Time
	Error      error
	Mutex      sync.RWMutex
}

// Simple download manager
type SimpleDownloadManager struct {
	downloads map[string]*SimpleManagedDownload
	mutex     sync.RWMutex
}

// NewSimpleDownloadManager creates a new simple download manager
func NewSimpleDownloadManager() *SimpleDownloadManager {
	return &SimpleDownloadManager{
		downloads: make(map[string]*SimpleManagedDownload),
	}
}

// AddDownload adds a new download
func (dm *SimpleDownloadManager) AddDownload(id string, dl *downloader.Downloader) *SimpleManagedDownload {
	managed := &SimpleManagedDownload{
		ID:         id,
		Downloader: dl,
		Status:     "downloading",
		StartTime:  time.Now(),
	}
	
	dm.mutex.Lock()
	dm.downloads[id] = managed
	dm.mutex.Unlock()
	
	return managed
}

// GetDownload retrieves a download
func (dm *SimpleDownloadManager) GetDownload(id string) (*SimpleManagedDownload, bool) {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	
	download, exists := dm.downloads[id]
	return download, exists
}

// GetAllDownloads returns all downloads
func (dm *SimpleDownloadManager) GetAllDownloads() map[string]*SimpleManagedDownload {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	
	result := make(map[string]*SimpleManagedDownload)
	for k, v := range dm.downloads {
		result[k] = v
	}
	return result
}

// Global simple download manager
var simpleDownloadManager = NewSimpleDownloadManager()

// Simple start download handler
func simpleStartDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	var req SimpleDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	
	// Set default threads
	if req.Threads <= 0 {
		req.Threads = 4
	}
	
	// Validate threads
	if req.Threads > 16 {
		http.Error(w, "Maximum 16 threads allowed", http.StatusBadRequest)
		return
	}
	
	// Generate unique ID
	downloadID := uuid.New().String()
	
	// Create unique filename
	filename := fmt.Sprintf("%s_%s", downloadID[:8], filepath.Base(req.Output))
	
	// Create downloader
	dl := downloader.NewDownloader(req.URL, filename, req.Threads)
	
	// Add to manager
	managed := simpleDownloadManager.AddDownload(downloadID, dl)
	
	// Start download in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("panic: %v", r)
				managed.Mutex.Unlock()
			}
		}()
		
		// Initialize progress
		if err := dl.LoadOrCreateProgress(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("failed to initialize download: %w", err)
			managed.Mutex.Unlock()
			return
		}
		
		// Start download
		if err := dl.Download(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("download failed: %w", err)
			managed.Mutex.Unlock()
			return
		}
		
		// Verify download
		if err := dl.VerifyDownload(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("verification failed: %w", err)
			managed.Mutex.Unlock()
			return
		}
		
		managed.Mutex.Lock()
		managed.Status = "completed"
		managed.Mutex.Unlock()
	}()
	
	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SimpleDownloadResponse{
		DownloadID: downloadID,
		Message:    "Download started successfully",
	})
}

// Simple get status handler
func simpleGetStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	// Extract ID from URL path
	path := r.URL.Path
	if len(path) < 20 || path[:20] != "/downloads/" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	
	downloadID := path[20:]
	if len(downloadID) > 0 && downloadID[len(downloadID)-7:] == "/status" {
		downloadID = downloadID[:len(downloadID)-7]
	}
	
	managed, exists := simpleDownloadManager.GetDownload(downloadID)
	if !exists {
		http.Error(w, "Download not found", http.StatusNotFound)
		return
	}
	
	managed.Mutex.RLock()
	defer managed.Mutex.RUnlock()
	
	status := SimpleDownloadStatus{
		DownloadID:  downloadID,
		URL:         managed.Downloader.URL,
		Filename:    managed.Downloader.Filename,
		Status:      managed.Status,
		ThreadsUsed: managed.Downloader.NumThreads,
		StartTime:   managed.StartTime.Format(time.RFC3339),
	}
	
	if managed.Error != nil {
		status.Error = managed.Error.Error()
	}
	
	// Get progress information
	if managed.Downloader.Progress != nil {
		status.PercentCompleted = managed.Downloader.Progress.GetOverallPercent()
		status.BytesDownloaded = managed.Downloader.Progress.GetTotalDownloaded()
		status.TotalSize = managed.Downloader.Progress.TotalSize
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Simple list downloads handler
func simpleListDownloadsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	downloads := simpleDownloadManager.GetAllDownloads()
	
	var statuses []SimpleDownloadStatus
	for id, managed := range downloads {
		managed.Mutex.RLock()
		
		status := SimpleDownloadStatus{
			DownloadID:  id,
			URL:         managed.Downloader.URL,
			Filename:    managed.Downloader.Filename,
			Status:      managed.Status,
			ThreadsUsed: managed.Downloader.NumThreads,
			StartTime:   managed.StartTime.Format(time.RFC3339),
		}
		
		if managed.Error != nil {
			status.Error = managed.Error.Error()
		}
		
		if managed.Downloader.Progress != nil {
			status.PercentCompleted = managed.Downloader.Progress.GetOverallPercent()
			status.BytesDownloaded = managed.Downloader.Progress.GetTotalDownloaded()
			status.TotalSize = managed.Downloader.Progress.TotalSize
		}
		
		statuses = append(statuses, status)
		managed.Mutex.RUnlock()
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"downloads": statuses,
		"count":     len(statuses),
	})
}

// Simple health handler
func simpleHealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0-simple",
	})
}

// Simple router
func simpleRouter(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	
	switch {
	case path == "/health":
		simpleHealthHandler(w, r)
	case path == "/downloads" && r.Method == http.MethodPost:
		simpleStartDownloadHandler(w, r)
	case path == "/downloads" && r.Method == http.MethodGet:
		simpleListDownloadsHandler(w, r)
	case len(path) > 20 && path[:20] == "/downloads/" && path[len(path)-7:] == "/status":
		simpleGetStatusHandler(w, r)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func main() {
	fmt.Println("Simple Multithreaded Downloader REST API Server")
	fmt.Println("===============================================")
	
	// Set up routes
	http.HandleFunc("/", simpleRouter)
	
	// Start server
	port := "8080"
	fmt.Printf("Server starting on port %s...\n", port)
	fmt.Printf("API endpoints available at http://localhost:%s\n", port)
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   /downloads           - Start a new download")
	fmt.Println("  GET    /downloads           - List all downloads")
	fmt.Println("  GET    /downloads/:id/status - Get download status")
	fmt.Println("  GET    /health              - Health check")
	
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
