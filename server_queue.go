package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// QueuedDownloadRequest represents the JSON request body for starting a queued download
type QueuedDownloadRequest struct {
	URL     string `json:"url" binding:"required"`
	Output  string `json:"output" binding:"required"`
	Threads int    `json:"threads"`
}

// QueuedDownloadResponse represents the response when enqueueing a download
type QueuedDownloadResponse struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// QueuedDownloadStatus represents the current status of a queued download
type QueuedDownloadStatus struct {
	JobID            string  `json:"job_id"`
	URL              string  `json:"url"`
	OutputPath       string  `json:"output_path"`
	Status           string  `json:"status"` // "queued", "processing", "completed", "failed"
	Progress         float64 `json:"progress"`
	BytesDownloaded  int64   `json:"bytes_downloaded"`
	TotalBytes       int64   `json:"total_bytes"`
	ThreadsUsed      int     `json:"threads_used"`
	CreatedAt        string  `json:"created_at"`
	StartedAt        string  `json:"started_at,omitempty"`
	CompletedAt      string  `json:"completed_at,omitempty"`
	WorkerID         string  `json:"worker_id,omitempty"`
	ErrorMessage     string  `json:"error_message,omitempty"`
}

// QueuedDownloadServer represents the main server with queue integration
type QueuedDownloadServer struct {
	queueManager *QueueManager
	dbManager    *DatabaseManager
	logger       *zap.Logger
	router       *gin.Engine
}

// NewQueuedDownloadServer creates a new server instance
func NewQueuedDownloadServer(queueManager *QueueManager, dbManager *DatabaseManager, logger *zap.Logger) *QueuedDownloadServer {
	server := &QueuedDownloadServer{
		queueManager: queueManager,
		dbManager:    dbManager,
		logger:       logger.With(zap.String("component", "server")),
	}
	
	server.setupRoutes()
	return server
}

// setupRoutes configures the HTTP routes
func (s *QueuedDownloadServer) setupRoutes() {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	router := gin.New()
	
	// Add middleware
	router.Use(s.loggingMiddleware())
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
		api.GET("/health", s.healthHandler)
		api.POST("/downloads", s.enqueueDownloadHandler)
		api.GET("/downloads", s.listDownloadsHandler)
		api.GET("/downloads/:id/status", s.getDownloadStatusHandler)
		api.GET("/queue/stats", s.getQueueStatsHandler)
		api.GET("/workers/stats", s.getWorkerStatsHandler)
	}
	
	// Legacy routes (without /api/v1 prefix) for backward compatibility
	router.POST("/downloads", s.enqueueDownloadHandler)
	router.GET("/downloads", s.listDownloadsHandler)
	router.GET("/downloads/:id/status", s.getDownloadStatusHandler)
	router.GET("/queue/stats", s.getQueueStatsHandler)
	router.GET("/workers/stats", s.getWorkerStatsHandler)
	router.GET("/health", s.healthHandler)
	
	s.router = router
}

// loggingMiddleware provides structured logging for HTTP requests
func (s *QueuedDownloadServer) loggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery
		
		// Process request
		c.Next()
		
		// Log request
		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()
		
		if raw != "" {
			path = path + "?" + raw
		}
		
		s.logger.Info("HTTP request",
			zap.String("method", method),
			zap.String("path", path),
			zap.String("client_ip", clientIP),
			zap.Int("status_code", statusCode),
			zap.Duration("latency", latency),
		)
	}
}

// enqueueDownloadHandler handles POST /downloads - enqueues a download job
func (s *QueuedDownloadServer) enqueueDownloadHandler(c *gin.Context) {
	var req QueuedDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("Invalid download request", zap.Error(err))
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
		s.logger.Warn("Too many threads requested", zap.Int("threads", req.Threads))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Maximum 16 threads allowed",
		})
		return
	}
	
	// Generate unique job ID
	jobID := uuid.New().String()
	
	// Create download job
	job := &DownloadJob{
		ID:         jobID,
		URL:        req.URL,
		OutputPath: req.Output,
		Threads:    req.Threads,
	}
	
	// Enqueue the job
	if err := s.queueManager.EnqueueJob(c.Request.Context(), job); err != nil {
		s.logger.Error("Failed to enqueue job", 
			zap.String("job_id", jobID),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to enqueue download job",
			"details": err.Error(),
		})
		return
	}
	
	s.logger.Info("Download job enqueued successfully",
		zap.String("job_id", jobID),
		zap.String("url", req.URL),
		zap.String("output", req.Output),
		zap.Int("threads", req.Threads))
	
	c.JSON(http.StatusCreated, QueuedDownloadResponse{
		JobID:   jobID,
		Message: "Download job enqueued successfully",
		Status:  "queued",
	})
}

// getDownloadStatusHandler handles GET /downloads/:id/status
func (s *QueuedDownloadServer) getDownloadStatusHandler(c *gin.Context) {
	jobID := c.Param("id")
	
	// Get status from queue (Redis)
	queueStatus, err := s.queueManager.GetJobStatus(c.Request.Context(), jobID)
	if err != nil {
		s.logger.Warn("Failed to get job status from queue", 
			zap.String("job_id", jobID),
			zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Download not found",
		})
		return
	}
	
	// Convert to response format
	status := QueuedDownloadStatus{
		JobID:           queueStatus.ID,
		Status:          queueStatus.Status,
		Progress:        queueStatus.Progress,
		BytesDownloaded: queueStatus.BytesDownloaded,
		TotalBytes:      queueStatus.TotalBytes,
		CreatedAt:       queueStatus.CreatedAt.Format(time.RFC3339),
		WorkerID:        queueStatus.WorkerID,
		ErrorMessage:    queueStatus.ErrorMessage,
	}
	
	if !queueStatus.StartedAt.IsZero() {
		status.StartedAt = queueStatus.StartedAt.Format(time.RFC3339)
	}
	
	if !queueStatus.CompletedAt.IsZero() {
		status.CompletedAt = queueStatus.CompletedAt.Format(time.RFC3339)
	}
	
	// Try to get additional info from database
	if dbRecord, err := s.dbManager.GetDownload(jobID); err == nil {
		status.URL = dbRecord.URL
		status.OutputPath = dbRecord.OutputPath
		status.ThreadsUsed = dbRecord.Threads
	}
	
	c.JSON(http.StatusOK, status)
}

// listDownloadsHandler handles GET /downloads - lists all downloads
func (s *QueuedDownloadServer) listDownloadsHandler(c *gin.Context) {
	// Get downloads from database
	downloads, err := s.dbManager.GetAllDownloads()
	if err != nil {
		s.logger.Error("Failed to get downloads from database", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve downloads",
			"details": err.Error(),
		})
		return
	}
	
	var statuses []QueuedDownloadStatus
	for _, download := range downloads {
		// Get queue status for each download
		queueStatus, err := s.queueManager.GetJobStatus(c.Request.Context(), download.ID)
		if err != nil {
			// If not in queue, use database info
			status := QueuedDownloadStatus{
				JobID:           download.ID,
				URL:             download.URL,
				OutputPath:      download.OutputPath,
				Status:          download.Status,
				BytesDownloaded: download.BytesDownloaded,
				TotalBytes:      download.TotalSize,
				ThreadsUsed:     download.Threads,
				CreatedAt:       download.CreatedAt.Format(time.RFC3339),
				ErrorMessage:    download.Error,
			}
			
			if download.TotalSize > 0 {
				status.Progress = float64(download.BytesDownloaded) / float64(download.TotalSize) * 100
			}
			
			statuses = append(statuses, status)
		} else {
			// Use queue status (more up-to-date)
			status := QueuedDownloadStatus{
				JobID:           queueStatus.ID,
				URL:             download.URL,
				OutputPath:      download.OutputPath,
				Status:          queueStatus.Status,
				Progress:        queueStatus.Progress,
				BytesDownloaded: queueStatus.BytesDownloaded,
				TotalBytes:      queueStatus.TotalBytes,
				ThreadsUsed:     download.Threads,
				CreatedAt:       queueStatus.CreatedAt.Format(time.RFC3339),
				WorkerID:        queueStatus.WorkerID,
				ErrorMessage:    queueStatus.ErrorMessage,
			}
			
			if !queueStatus.StartedAt.IsZero() {
				status.StartedAt = queueStatus.StartedAt.Format(time.RFC3339)
			}
			
			if !queueStatus.CompletedAt.IsZero() {
				status.CompletedAt = queueStatus.CompletedAt.Format(time.RFC3339)
			}
			
			statuses = append(statuses, status)
		}
	}
	
	c.JSON(http.StatusOK, gin.H{
		"downloads": statuses,
		"count":     len(statuses),
	})
}

// getQueueStatsHandler handles GET /queue/stats
func (s *QueuedDownloadServer) getQueueStatsHandler(c *gin.Context) {
	stats, err := s.queueManager.GetQueueStats(c.Request.Context())
	if err != nil {
		s.logger.Error("Failed to get queue statistics", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to get queue statistics",
			"details": err.Error(),
		})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"queue_stats": stats,
		"timestamp":   time.Now().Format(time.RFC3339),
	})
}

// getWorkerStatsHandler handles GET /workers/stats
func (s *QueuedDownloadServer) getWorkerStatsHandler(c *gin.Context) {
	// This would typically come from a worker manager instance
	// For now, return basic info
	c.JSON(http.StatusOK, gin.H{
		"worker_stats": gin.H{
			"message": "Worker stats available when running with worker manager",
		},
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// healthHandler handles GET /health
func (s *QueuedDownloadServer) healthHandler(c *gin.Context) {
	// Check Redis connection
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	
	redisHealthy := true
	if err := s.queueManager.client.Ping(ctx).Err(); err != nil {
		redisHealthy = false
		s.logger.Warn("Redis health check failed", zap.Error(err))
	}
	
	// Check database connection
	dbHealthy := true
	if s.dbManager != nil {
		if sqlDB, err := s.dbManager.db.DB(); err == nil {
			if err := sqlDB.PingContext(ctx); err != nil {
				dbHealthy = false
				s.logger.Warn("Database health check failed", zap.Error(err))
			}
		} else {
			dbHealthy = false
			s.logger.Warn("Failed to get database connection", zap.Error(err))
		}
	}
	
	status := "healthy"
	httpStatus := http.StatusOK
	
	if !redisHealthy || !dbHealthy {
		status = "unhealthy"
		httpStatus = http.StatusServiceUnavailable
	}
	
	c.JSON(httpStatus, gin.H{
		"status":    status,
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "2.0.0-queue",
		"checks": gin.H{
			"redis":    redisHealthy,
			"database": dbHealthy,
		},
	})
}

// Run starts the HTTP server
func (s *QueuedDownloadServer) Run(port string) error {
	s.logger.Info("Starting queued download server", zap.String("port", port))
	return s.router.Run(":" + port)
}

// main function for running the queued server
func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic(fmt.Sprintf("Failed to initialize logger: %v", err))
	}
	defer logger.Sync()
	
	// Configuration from environment variables
	redisURL := getEnv("REDIS_URL", "redis://localhost:6379")
	postgresURL := getEnv("POSTGRES_URL", "postgres://user:password@localhost/downloads?sslmode=disable")
	port := getEnv("PORT", "8080")
	
	logger.Info("Starting queued download server",
		zap.String("redis_url", redisURL),
		zap.String("port", port))
	
	// Initialize queue manager
	queueManager, err := NewQueueManager(redisURL, logger)
	if err != nil {
		logger.Fatal("Failed to initialize queue manager", zap.Error(err))
	}
	defer queueManager.Close()
	
	// Initialize database manager
	if err := InitPostgreSQLDatabase(postgresURL); err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}
	defer func() {
		if dbManager != nil {
			dbManager.Close()
		}
	}()
	
	// Create and start server
	server := NewQueuedDownloadServer(queueManager, dbManager, logger)
	
	logger.Info("Queued download server starting",
		zap.String("port", port),
		zap.String("mode", "queue-based"))
	
	fmt.Println("Queued Multithreaded Downloader REST API Server")
	fmt.Println("===============================================")
	fmt.Printf("Server starting on port %s...\n", port)
	fmt.Printf("API endpoints available at http://localhost:%s\n", port)
	fmt.Println("\nAvailable endpoints:")
	fmt.Println("  POST   /downloads           - Enqueue a new download")
	fmt.Println("  GET    /downloads           - List all downloads")
	fmt.Println("  GET    /downloads/:id/status - Get download status")
	fmt.Println("  GET    /queue/stats         - Get queue statistics")
	fmt.Println("  GET    /workers/stats       - Get worker statistics")
	fmt.Println("  GET    /health              - Health check")
	fmt.Println("\nNote: This server enqueues jobs. Start workers separately to process downloads.")
	
	if err := server.Run(port); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
