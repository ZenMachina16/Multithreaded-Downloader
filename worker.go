package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"multithreaded-downloader/downloader"
)

// Worker represents a download worker
type Worker struct {
	ID           string
	queueManager *QueueManager
	dbManager    *DatabaseManager
	logger       *zap.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           *sync.WaitGroup
}

// WorkerManager manages multiple workers
type WorkerManager struct {
	workers      []*Worker
	queueManager *QueueManager
	dbManager    *DatabaseManager
	logger       *zap.Logger
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewWorker creates a new worker instance
func NewWorker(queueManager *QueueManager, dbManager *DatabaseManager, logger *zap.Logger) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &Worker{
		ID:           uuid.New().String(),
		queueManager: queueManager,
		dbManager:    dbManager,
		logger:       logger.With(zap.String("component", "worker")),
		ctx:          ctx,
		cancel:       cancel,
		wg:           &sync.WaitGroup{},
	}
}

// Start begins the worker's job processing loop
func (w *Worker) Start() {
	w.wg.Add(1)
	go w.processJobs()
	
	w.logger.Info("Worker started", zap.String("worker_id", w.ID))
}

// Stop gracefully stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping worker", zap.String("worker_id", w.ID))
	w.cancel()
	w.wg.Wait()
	w.logger.Info("Worker stopped", zap.String("worker_id", w.ID))
}

// processJobs is the main worker loop that processes jobs from the queue
func (w *Worker) processJobs() {
	defer w.wg.Done()
	
	w.logger.Info("Worker processing loop started", zap.String("worker_id", w.ID))
	
	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Worker context cancelled, stopping", zap.String("worker_id", w.ID))
			return
		default:
			// Try to get a job from the queue
			job, err := w.queueManager.DequeueJob(w.ctx, w.ID)
			if err != nil {
				w.logger.Error("Failed to dequeue job", 
					zap.String("worker_id", w.ID),
					zap.Error(err))
				time.Sleep(5 * time.Second)
				continue
			}
			
			if job == nil {
				// No jobs available, continue polling
				continue
			}
			
			// Process the job
			w.processDownloadJob(job)
		}
	}
}

// processDownloadJob processes a single download job
func (w *Worker) processDownloadJob(job *DownloadJob) {
	jobLogger := w.logger.With(
		zap.String("job_id", job.ID),
		zap.String("worker_id", w.ID),
		zap.String("url", job.URL),
		zap.String("output_path", job.OutputPath),
		zap.Int("threads", job.Threads),
	)
	
	jobLogger.Info("Processing download job started")
	
	// Create database record
	dbRecord, err := w.dbManager.CreateDownload(job.ID, job.URL, job.OutputPath, job.Threads)
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to create database record: %v", err)
		jobLogger.Error("Database record creation failed", zap.Error(err))
		w.queueManager.FailJob(context.Background(), job.ID, w.ID, errorMsg)
		return
	}
	
	// Create downloader instance
	dl := downloader.NewDownloader(job.URL, job.OutputPath, job.Threads)
	
	// Set up progress tracking
	progressCtx, progressCancel := context.WithCancel(context.Background())
	defer progressCancel()
	
	// Start progress tracking goroutine
	go w.trackProgress(progressCtx, job.ID, dl, jobLogger)
	
	jobLogger.Info("Starting download process")
	
	// Initialize downloader progress
	if err := dl.LoadOrCreateProgress(); err != nil {
		errorMsg := fmt.Sprintf("Failed to initialize download: %v", err)
		jobLogger.Error("Download initialization failed", zap.Error(err))
		w.dbManager.UpdateDownloadStatus(job.ID, "failed", errorMsg)
		w.queueManager.FailJob(context.Background(), job.ID, w.ID, errorMsg)
		return
	}
	
	// Start the download
	if err := dl.Download(); err != nil {
		errorMsg := fmt.Sprintf("Download failed: %v", err)
		jobLogger.Error("Download execution failed", zap.Error(err))
		w.dbManager.UpdateDownloadStatus(job.ID, "failed", errorMsg)
		w.queueManager.FailJob(context.Background(), job.ID, w.ID, errorMsg)
		return
	}
	
	// Verify the download
	if err := dl.VerifyDownload(); err != nil {
		errorMsg := fmt.Sprintf("Download verification failed: %v", err)
		jobLogger.Error("Download verification failed", zap.Error(err))
		w.dbManager.UpdateDownloadStatus(job.ID, "failed", errorMsg)
		w.queueManager.FailJob(context.Background(), job.ID, w.ID, errorMsg)
		return
	}
	
	// Mark as completed
	if err := w.dbManager.UpdateDownloadStatus(job.ID, "completed", ""); err != nil {
		jobLogger.Warn("Failed to update database status to completed", zap.Error(err))
	}
	
	if err := w.queueManager.CompleteJob(context.Background(), job.ID, w.ID); err != nil {
		jobLogger.Warn("Failed to mark job as completed in queue", zap.Error(err))
	}
	
	// Final progress update
	if dl.Progress != nil {
		w.queueManager.UpdateJobProgress(context.Background(), job.ID, 100.0, dl.Progress.TotalSize, dl.Progress.TotalSize)
		w.dbManager.UpdateDownloadProgress(job.ID, dl.Progress.TotalSize, dl.Progress.TotalSize, "completed")
	}
	
	jobLogger.Info("Download job completed successfully",
		zap.Duration("processing_time", time.Since(job.StartedAt)))
}

// trackProgress monitors download progress and updates both database and queue
func (w *Worker) trackProgress(ctx context.Context, jobID string, dl *downloader.Downloader, logger *zap.Logger) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if dl.Progress == nil {
				continue
			}
			
			bytesDownloaded := dl.Progress.GetTotalDownloaded()
			totalBytes := dl.Progress.TotalSize
			progress := dl.Progress.GetOverallPercent()
			
			// Update queue progress
			if err := w.queueManager.UpdateJobProgress(ctx, jobID, progress, bytesDownloaded, totalBytes); err != nil {
				logger.Warn("Failed to update queue progress", zap.Error(err))
			}
			
			// Update database progress
			if err := w.dbManager.UpdateDownloadProgress(jobID, bytesDownloaded, totalBytes, "downloading"); err != nil {
				logger.Warn("Failed to update database progress", zap.Error(err))
			}
			
			logger.Debug("Progress updated",
				zap.Float64("progress", progress),
				zap.Int64("bytes_downloaded", bytesDownloaded),
				zap.Int64("total_bytes", totalBytes))
		}
	}
}

// NewWorkerManager creates a new worker manager
func NewWorkerManager(numWorkers int, queueManager *QueueManager, dbManager *DatabaseManager, logger *zap.Logger) *WorkerManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	wm := &WorkerManager{
		workers:      make([]*Worker, 0, numWorkers),
		queueManager: queueManager,
		dbManager:    dbManager,
		logger:       logger.With(zap.String("component", "worker_manager")),
		ctx:          ctx,
		cancel:       cancel,
	}
	
	// Create workers
	for i := 0; i < numWorkers; i++ {
		worker := NewWorker(queueManager, dbManager, logger)
		wm.workers = append(wm.workers, worker)
	}
	
	return wm
}

// Start starts all workers
func (wm *WorkerManager) Start() {
	wm.logger.Info("Starting worker manager", zap.Int("worker_count", len(wm.workers)))
	
	// Start all workers
	for _, worker := range wm.workers {
		worker.Start()
	}
	
	// Start cleanup routine
	wm.wg.Add(1)
	go wm.cleanupRoutine()
	
	wm.logger.Info("All workers started successfully")
}

// Stop gracefully stops all workers
func (wm *WorkerManager) Stop() {
	wm.logger.Info("Stopping worker manager")
	
	// Cancel context to signal all workers to stop
	wm.cancel()
	
	// Stop all workers
	for _, worker := range wm.workers {
		worker.Stop()
	}
	
	// Wait for cleanup routine to finish
	wm.wg.Wait()
	
	wm.logger.Info("Worker manager stopped successfully")
}

// cleanupRoutine periodically cleans up stale jobs
func (wm *WorkerManager) cleanupRoutine() {
	defer wm.wg.Done()
	
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	
	for {
		select {
		case <-wm.ctx.Done():
			return
		case <-ticker.C:
			if err := wm.queueManager.CleanupStaleJobs(wm.ctx); err != nil {
				wm.logger.Error("Failed to cleanup stale jobs", zap.Error(err))
			}
		}
	}
}

// GetWorkerStats returns statistics about the workers
func (wm *WorkerManager) GetWorkerStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total_workers": len(wm.workers),
		"active_workers": len(wm.workers), // All workers are considered active if started
		"worker_ids": make([]string, len(wm.workers)),
	}
	
	for i, worker := range wm.workers {
		stats["worker_ids"].([]string)[i] = worker.ID
	}
	
	return stats
}

// main function for running workers standalone
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
	numWorkers := 3 // Default number of workers
	
	logger.Info("Starting download workers",
		zap.String("redis_url", redisURL),
		zap.Int("num_workers", numWorkers))
	
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
	
	// Create worker manager
	workerManager := NewWorkerManager(numWorkers, queueManager, dbManager, logger)
	
	// Start workers
	workerManager.Start()
	
	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	
	logger.Info("Workers started, waiting for shutdown signal...")
	
	// Wait for shutdown signal
	<-sigChan
	logger.Info("Shutdown signal received, stopping workers...")
	
	// Stop workers gracefully
	workerManager.Stop()
	
	logger.Info("All workers stopped, exiting")
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
