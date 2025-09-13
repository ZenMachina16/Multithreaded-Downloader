package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"multithreaded-downloader/downloader"
)

// DownloadRequest represents the JSON request body for starting a download
type DownloadRequest struct {
	URL     string `json:"url" binding:"required"`
	Output  string `json:"output" binding:"required"`
	Threads int    `json:"threads"`
}

// DownloadResponse represents the response when starting a download
type DownloadResponse struct {
	DownloadID string `json:"download_id"`
	Message    string `json:"message"`
}

// DownloadStatus represents the current status of a download
type DownloadStatus struct {
	DownloadID       string  `json:"download_id"`
	URL              string  `json:"url"`
	Filename         string  `json:"filename"`
	Status           string  `json:"status"` // "downloading", "paused", "completed", "failed"
	PercentCompleted float64 `json:"percent_completed"`
	BytesDownloaded  int64   `json:"bytes_downloaded"`
	TotalSize        int64   `json:"total_size"`
	ThreadsUsed      int     `json:"threads_used"`
	StartTime        string  `json:"start_time"`
	Error            string  `json:"error,omitempty"`
}

// ManagedDownload wraps a downloader with additional management info
type ManagedDownload struct {
	ID         string
	Downloader *downloader.Downloader
	Status     string
	StartTime  time.Time
	Context    context.Context
	Cancel     context.CancelFunc
	Error      error
	Mutex      sync.RWMutex
}

// DownloadManager manages multiple concurrent downloads
type DownloadManager struct {
	downloads map[string]*ManagedDownload
	mutex     sync.RWMutex
}

// NewDownloadManager creates a new download manager
func NewDownloadManager() *DownloadManager {
	return &DownloadManager{
		downloads: make(map[string]*ManagedDownload),
	}
}

// AddDownload adds a new download to the manager
func (dm *DownloadManager) AddDownload(id string, dl *downloader.Downloader) *ManagedDownload {
	ctx, cancel := context.WithCancel(context.Background())
	
	managed := &ManagedDownload{
		ID:         id,
		Downloader: dl,
		Status:     "downloading",
		StartTime:  time.Now(),
		Context:    ctx,
		Cancel:     cancel,
	}
	
	dm.mutex.Lock()
	dm.downloads[id] = managed
	dm.mutex.Unlock()
	
	return managed
}

// GetDownload retrieves a download by ID
func (dm *DownloadManager) GetDownload(id string) (*ManagedDownload, bool) {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	
	download, exists := dm.downloads[id]
	return download, exists
}

// RemoveDownload removes a completed download from the manager
func (dm *DownloadManager) RemoveDownload(id string) {
	dm.mutex.Lock()
	defer dm.mutex.Unlock()
	
	if download, exists := dm.downloads[id]; exists {
		download.Cancel()
		delete(dm.downloads, id)
	}
}

// GetAllDownloads returns all active downloads
func (dm *DownloadManager) GetAllDownloads() map[string]*ManagedDownload {
	dm.mutex.RLock()
	defer dm.mutex.RUnlock()
	
	result := make(map[string]*ManagedDownload)
	for k, v := range dm.downloads {
		result[k] = v
	}
	return result
}

// Global download manager instance
var downloadManager = NewDownloadManager()

// startDownloadHandler handles POST /downloads
func startDownloadHandler(c *gin.Context) {
	var req DownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid request body",
			"details": err.Error(),
		})
		return
	}
	
	// Set default threads if not specified
	if req.Threads <= 0 {
		req.Threads = 4
	}
	
	// Validate threads count
	if req.Threads > 16 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Maximum 16 threads allowed",
		})
		return
	}
	
	// Generate unique download ID
	downloadID := uuid.New().String()
	
	// Create a unique filename to avoid conflicts
	filename := fmt.Sprintf("%s_%s", downloadID[:8], filepath.Base(req.Output))
	
	// Create downloader instance
	dl := downloader.NewDownloader(req.URL, filename, req.Threads)
	
	// Add to manager
	managed := downloadManager.AddDownload(downloadID, dl)
	
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
	
	c.JSON(http.StatusCreated, DownloadResponse{
		DownloadID: downloadID,
		Message:    "Download started successfully",
	})
}

// getDownloadStatusHandler handles GET /downloads/:id/status
func getDownloadStatusHandler(c *gin.Context) {
	downloadID := c.Param("id")
	
	managed, exists := downloadManager.GetDownload(downloadID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Download not found",
		})
		return
	}
	
	managed.Mutex.RLock()
	defer managed.Mutex.RUnlock()
	
	status := DownloadStatus{
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
	
	// Get progress information if available
	if managed.Downloader.Progress != nil {
		status.PercentCompleted = managed.Downloader.Progress.GetOverallPercent()
		status.BytesDownloaded = managed.Downloader.Progress.GetTotalDownloaded()
		status.TotalSize = managed.Downloader.Progress.TotalSize
	}
	
	c.JSON(http.StatusOK, status)
}

// pauseDownloadHandler handles POST /downloads/:id/pause
func pauseDownloadHandler(c *gin.Context) {
	downloadID := c.Param("id")
	
	managed, exists := downloadManager.GetDownload(downloadID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Download not found",
		})
		return
	}
	
	managed.Mutex.Lock()
	defer managed.Mutex.Unlock()
	
	if managed.Status == "completed" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Cannot pause completed download",
		})
		return
	}
	
	if managed.Status == "paused" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Download is already paused",
		})
		return
	}
	
	if managed.Status == "failed" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Cannot pause failed download",
		})
		return
	}
	
	// Cancel the download context to pause it
	managed.Cancel()
	managed.Status = "paused"
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Download paused successfully",
	})
}

// resumeDownloadHandler handles POST /downloads/:id/resume
func resumeDownloadHandler(c *gin.Context) {
	downloadID := c.Param("id")
	
	managed, exists := downloadManager.GetDownload(downloadID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Download not found",
		})
		return
	}
	
	managed.Mutex.Lock()
	defer managed.Mutex.Unlock()
	
	if managed.Status != "paused" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Download is not paused",
		})
		return
	}
	
	// Create new context for resuming
	ctx, cancel := context.WithCancel(context.Background())
	managed.Context = ctx
	managed.Cancel = cancel
	managed.Status = "downloading"
	managed.Error = nil
	
	// Resume download in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("panic: %v", r)
				managed.Mutex.Unlock()
			}
		}()
		
		// Resume download
		if err := managed.Downloader.Download(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("resume failed: %w", err)
			managed.Mutex.Unlock()
			return
		}
		
		// Verify download
		if err := managed.Downloader.VerifyDownload(); err != nil {
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
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Download resumed successfully",
	})
}

// listDownloadsHandler handles GET /downloads (bonus endpoint)
func listDownloadsHandler(c *gin.Context) {
	downloads := downloadManager.GetAllDownloads()
	
	var statuses []DownloadStatus
	for id, managed := range downloads {
		managed.Mutex.RLock()
		
		status := DownloadStatus{
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
	
	c.JSON(http.StatusOK, gin.H{
		"downloads": statuses,
		"count":     len(statuses),
	})
}

// deleteDownloadHandler handles DELETE /downloads/:id (bonus endpoint)
func deleteDownloadHandler(c *gin.Context) {
	downloadID := c.Param("id")
	
	managed, exists := downloadManager.GetDownload(downloadID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Download not found",
		})
		return
	}
	
	managed.Mutex.Lock()
	defer managed.Mutex.Unlock()
	
	// Cancel the download if it's still running
	if managed.Status == "downloading" {
		managed.Cancel()
	}
	
	// Remove from manager
	downloadManager.RemoveDownload(downloadID)
	
	c.JSON(http.StatusOK, gin.H{
		"message": "Download removed successfully",
	})
}

// healthHandler handles GET /health
func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
	})
}

func setupRoutes() *gin.Engine {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	router := gin.New()
	
	// Add middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	
	// Add CORS middleware for web clients
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		
		c.Next()
	})
	
	// API routes
	api := router.Group("/api/v1")
	{
		api.GET("/health", healthHandler)
		api.POST("/downloads", startDownloadHandler)
		api.GET("/downloads", listDownloadsHandler)
		api.GET("/downloads/:id/status", getDownloadStatusHandler)
		api.POST("/downloads/:id/pause", pauseDownloadHandler)
		api.POST("/downloads/:id/resume", resumeDownloadHandler)
		api.DELETE("/downloads/:id", deleteDownloadHandler)
	}
	
	// Legacy routes (without /api/v1 prefix) for backward compatibility
	router.POST("/downloads", startDownloadHandler)
	router.GET("/downloads", listDownloadsHandler)
	router.GET("/downloads/:id/status", getDownloadStatusHandler)
	router.POST("/downloads/:id/pause", pauseDownloadHandler)
	router.POST("/downloads/:id/resume", resumeDownloadHandler)
	router.DELETE("/downloads/:id", deleteDownloadHandler)
	router.GET("/health", healthHandler)
	
	return router
}

func main() {
	fmt.Println("Multithreaded Downloader REST API Server")
	fmt.Println("========================================")
	
	router := setupRoutes()
	
	// Start server
	port := "8080"
	fmt.Printf("Server starting on port %s...\n", port)
	fmt.Printf("API endpoints available at http://localhost:%s\n", port)
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   /downloads           - Start a new download")
	fmt.Println("  GET    /downloads           - List all downloads")
	fmt.Println("  GET    /downloads/:id/status - Get download status")
	fmt.Println("  POST   /downloads/:id/pause  - Pause a download")
	fmt.Println("  POST   /downloads/:id/resume - Resume a download")
	fmt.Println("  DELETE /downloads/:id        - Remove a download")
	fmt.Println("  GET    /health              - Health check")
	
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
