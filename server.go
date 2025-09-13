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
	// Database record reference
	DBRecord   *Download
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
func (dm *DownloadManager) AddDownload(id string, dl *downloader.Downloader, dbRecord *Download) *ManagedDownload {
	ctx, cancel := context.WithCancel(context.Background())
	
	managed := &ManagedDownload{
		ID:         id,
		Downloader: dl,
		Status:     "downloading",
		StartTime:  time.Now(),
		Context:    ctx,
		Cancel:     cancel,
		DBRecord:   dbRecord,
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
	
	// Save to database
	dbRecord, err := SaveDownload(downloadID, req.URL, filename, req.Threads)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to save download to database",
			"details": err.Error(),
		})
		return
	}
	
	// Add to manager
	managed := downloadManager.AddDownload(downloadID, dl, dbRecord)
	
	// Start download in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("panic: %v", r)
				// Update database
				UpdateStatus(downloadID, "failed", managed.Error.Error())
				managed.Mutex.Unlock()
			}
		}()
		
		// Start periodic progress updates to database
		progressTicker := time.NewTicker(3 * time.Second)
		defer progressTicker.Stop()
		
		go func() {
			for {
				select {
				case <-managed.Context.Done():
					return
				case <-progressTicker.C:
					managed.Mutex.RLock()
					if managed.Downloader.Progress != nil {
						bytesDownloaded := managed.Downloader.Progress.GetTotalDownloaded()
						totalBytes := managed.Downloader.Progress.TotalSize
						status := managed.Status
						UpdateProgress(downloadID, bytesDownloaded, totalBytes, status)
					}
					managed.Mutex.RUnlock()
				}
			}
		}()
		
		// Initialize progress
		if err := dl.LoadOrCreateProgress(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("failed to initialize download: %w", err)
			// Update database
			UpdateStatus(downloadID, "failed", managed.Error.Error())
			managed.Mutex.Unlock()
			return
		}
		
		// Start download
		if err := dl.Download(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("download failed: %w", err)
			// Update database
			UpdateStatus(downloadID, "failed", managed.Error.Error())
			managed.Mutex.Unlock()
			return
		}
		
		// Verify download
		if err := dl.VerifyDownload(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("verification failed: %w", err)
			// Update database
			UpdateStatus(downloadID, "failed", managed.Error.Error())
			managed.Mutex.Unlock()
			return
		}
		
		managed.Mutex.Lock()
		managed.Status = "completed"
		// Update database with completion
		if managed.Downloader.Progress != nil {
			UpdateProgress(downloadID, managed.Downloader.Progress.TotalSize, managed.Downloader.Progress.TotalSize, "completed")
		} else {
			UpdateStatus(downloadID, "completed", "")
		}
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
	
	// Update database
	if managed.Downloader.Progress != nil {
		UpdateProgress(downloadID, managed.Downloader.Progress.GetTotalDownloaded(), managed.Downloader.Progress.TotalSize, "paused")
	} else {
		UpdateStatus(downloadID, "paused", "")
	}
	
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
	
	// Update database status
	UpdateStatus(downloadID, "downloading", "")
	
	// Resume download in goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("panic: %v", r)
				// Update database
				UpdateStatus(downloadID, "failed", managed.Error.Error())
				managed.Mutex.Unlock()
			}
		}()
		
		// Restart periodic progress updates
		progressTicker := time.NewTicker(3 * time.Second)
		defer progressTicker.Stop()
		
		go func() {
			for {
				select {
				case <-managed.Context.Done():
					return
				case <-progressTicker.C:
					managed.Mutex.RLock()
					if managed.Downloader.Progress != nil {
						bytesDownloaded := managed.Downloader.Progress.GetTotalDownloaded()
						totalBytes := managed.Downloader.Progress.TotalSize
						status := managed.Status
						UpdateProgress(downloadID, bytesDownloaded, totalBytes, status)
					}
					managed.Mutex.RUnlock()
				}
			}
		}()
		
		// Resume download
		if err := managed.Downloader.Download(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("resume failed: %w", err)
			// Update database
			UpdateStatus(downloadID, "failed", managed.Error.Error())
			managed.Mutex.Unlock()
			return
		}
		
		// Verify download
		if err := managed.Downloader.VerifyDownload(); err != nil {
			managed.Mutex.Lock()
			managed.Status = "failed"
			managed.Error = fmt.Errorf("verification failed: %w", err)
			// Update database
			UpdateStatus(downloadID, "failed", managed.Error.Error())
			managed.Mutex.Unlock()
			return
		}
		
		managed.Mutex.Lock()
		managed.Status = "completed"
		// Update database with completion
		if managed.Downloader.Progress != nil {
			UpdateProgress(downloadID, managed.Downloader.Progress.TotalSize, managed.Downloader.Progress.TotalSize, "completed")
		} else {
			UpdateStatus(downloadID, "completed", "")
		}
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
	
	// Remove from manager and database
	downloadManager.RemoveDownload(downloadID)
	RemoveDownload(downloadID)
	
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
		api.GET("/stats", statsHandler)
	}
	
	// Legacy routes (without /api/v1 prefix) for backward compatibility
	router.POST("/downloads", startDownloadHandler)
	router.GET("/downloads", listDownloadsHandler)
	router.GET("/downloads/:id/status", getDownloadStatusHandler)
	router.POST("/downloads/:id/pause", pauseDownloadHandler)
	router.POST("/downloads/:id/resume", resumeDownloadHandler)
	router.DELETE("/downloads/:id", deleteDownloadHandler)
	router.GET("/stats", statsHandler)
	router.GET("/health", healthHandler)
	
	return router
}

// resumeIncompleteDownloads loads incomplete downloads from database and resumes them
func resumeIncompleteDownloads() {
	fmt.Println("Checking for incomplete downloads to resume...")
	
	incompleteDownloads, err := GetIncompleteDownloadsFromDB()
	if err != nil {
		fmt.Printf("Error loading incomplete downloads: %v\n", err)
		return
	}
	
	if len(incompleteDownloads) == 0 {
		fmt.Println("No incomplete downloads found.")
		return
	}
	
	fmt.Printf("Found %d incomplete downloads. Resuming...\n", len(incompleteDownloads))
	
	for _, dbRecord := range incompleteDownloads {
		// Create downloader instance
		dl := downloader.NewDownloader(dbRecord.URL, dbRecord.OutputPath, dbRecord.Threads)
		
		// Add to manager
		managed := downloadManager.AddDownload(dbRecord.ID, dl, &dbRecord)
		
		// Start download in goroutine
		go func(downloadID string, managed *ManagedDownload) {
			defer func() {
				if r := recover(); r != nil {
					managed.Mutex.Lock()
					managed.Status = "failed"
					managed.Error = fmt.Errorf("panic during resume: %v", r)
					UpdateStatus(downloadID, "failed", managed.Error.Error())
					managed.Mutex.Unlock()
				}
			}()
			
			// Start periodic progress updates to database
			progressTicker := time.NewTicker(3 * time.Second)
			defer progressTicker.Stop()
			
			go func() {
				for {
					select {
					case <-managed.Context.Done():
						return
					case <-progressTicker.C:
						managed.Mutex.RLock()
						if managed.Downloader.Progress != nil {
							bytesDownloaded := managed.Downloader.Progress.GetTotalDownloaded()
							totalBytes := managed.Downloader.Progress.TotalSize
							status := managed.Status
							UpdateProgress(downloadID, bytesDownloaded, totalBytes, status)
						}
						managed.Mutex.RUnlock()
					}
				}
			}()
			
			// Load existing progress
			if err := dl.LoadOrCreateProgress(); err != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("failed to load progress: %w", err)
				UpdateStatus(downloadID, "failed", managed.Error.Error())
				managed.Mutex.Unlock()
				return
			}
			
			// Resume download
			if err := dl.Download(); err != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("resume failed: %w", err)
				UpdateStatus(downloadID, "failed", managed.Error.Error())
				managed.Mutex.Unlock()
				return
			}
			
			// Verify download
			if err := dl.VerifyDownload(); err != nil {
				managed.Mutex.Lock()
				managed.Status = "failed"
				managed.Error = fmt.Errorf("verification failed: %w", err)
				UpdateStatus(downloadID, "failed", managed.Error.Error())
				managed.Mutex.Unlock()
				return
			}
			
			managed.Mutex.Lock()
			managed.Status = "completed"
			// Update database with completion
			if managed.Downloader.Progress != nil {
				UpdateProgress(downloadID, managed.Downloader.Progress.TotalSize, managed.Downloader.Progress.TotalSize, "completed")
			} else {
				UpdateStatus(downloadID, "completed", "")
			}
			managed.Mutex.Unlock()
			
			fmt.Printf("Resumed download completed: %s\n", downloadID)
		}(dbRecord.ID, managed)
		
		fmt.Printf("Resumed download: %s (%s)\n", dbRecord.ID, dbRecord.URL)
	}
}

// statsHandler handles GET /stats (bonus endpoint)
func statsHandler(c *gin.Context) {
	if dbManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Database not available",
		})
		return
	}
	
	stats, err := dbManager.GetDownloadStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to get download statistics",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"statistics": stats,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func main() {
	fmt.Println("Multithreaded Downloader REST API Server")
	fmt.Println("========================================")
	
	// Initialize database
	if err := InitDatabase("downloads.db"); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		if dbManager != nil {
			dbManager.Close()
		}
	}()
	
	// Resume incomplete downloads
	resumeIncompleteDownloads()
	
	// Start cleanup routine for old completed downloads
	go func() {
		ticker := time.NewTicker(24 * time.Hour) // Clean up daily
		defer ticker.Stop()
		
		for range ticker.C {
			if err := dbManager.CleanupCompletedDownloads(7 * 24 * time.Hour); err != nil {
				fmt.Printf("Error during cleanup: %v\n", err)
			}
		}
	}()
	
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
	fmt.Println("  GET    /stats               - Download statistics")
	fmt.Println("  GET    /health              - Health check")
	
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
