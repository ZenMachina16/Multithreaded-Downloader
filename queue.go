package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// Redis keys
	DownloadJobsQueue    = "download_jobs"
	ProcessingJobsQueue  = "processing_jobs"
	CompletedJobsQueue   = "completed_jobs"
	FailedJobsQueue      = "failed_jobs"
	
	// Job timeouts
	JobProcessingTimeout = 30 * time.Minute
	QueuePollTimeout     = 10 * time.Second
)

// DownloadJob represents a job in the queue
type DownloadJob struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	OutputPath string    `json:"output_path"`
	Threads    int       `json:"threads"`
	CreatedAt  time.Time `json:"created_at"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	WorkerID   string    `json:"worker_id,omitempty"`
}

// JobStatus represents the status of a job
type JobStatus struct {
	ID              string    `json:"id"`
	Status          string    `json:"status"` // "queued", "processing", "completed", "failed"
	Progress        float64   `json:"progress"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	TotalBytes      int64     `json:"total_bytes"`
	ErrorMessage    string    `json:"error_message,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	StartedAt       time.Time `json:"started_at,omitempty"`
	CompletedAt     time.Time `json:"completed_at,omitempty"`
	WorkerID        string    `json:"worker_id,omitempty"`
}

// QueueManager handles Redis queue operations
type QueueManager struct {
	client *redis.Client
	logger *zap.Logger
}

// NewQueueManager creates a new queue manager
func NewQueueManager(redisURL string, logger *zap.Logger) (*QueueManager, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}
	
	client := redis.NewClient(opts)
	
	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}
	
	logger.Info("Connected to Redis successfully", zap.String("addr", opts.Addr))
	
	return &QueueManager{
		client: client,
		logger: logger,
	}, nil
}

// EnqueueJob adds a new download job to the queue
func (qm *QueueManager) EnqueueJob(ctx context.Context, job *DownloadJob) error {
	job.CreatedAt = time.Now()
	
	jobData, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}
	
	// Add to the main queue
	if err := qm.client.LPush(ctx, DownloadJobsQueue, jobData).Err(); err != nil {
		return fmt.Errorf("failed to enqueue job: %w", err)
	}
	
	// Set initial status
	status := &JobStatus{
		ID:        job.ID,
		Status:    "queued",
		CreatedAt: job.CreatedAt,
	}
	
	if err := qm.SetJobStatus(ctx, status); err != nil {
		qm.logger.Warn("Failed to set initial job status", 
			zap.String("job_id", job.ID),
			zap.Error(err))
	}
	
	qm.logger.Info("Job enqueued successfully", 
		zap.String("job_id", job.ID),
		zap.String("url", job.URL),
		zap.Int("threads", job.Threads))
	
	return nil
}

// DequeueJob retrieves and removes a job from the queue (blocking operation)
func (qm *QueueManager) DequeueJob(ctx context.Context, workerID string) (*DownloadJob, error) {
	// Use BRPOPLPUSH for reliable queue processing
	// This atomically moves the job from the main queue to a processing queue
	result, err := qm.client.BRPopLPush(ctx, DownloadJobsQueue, ProcessingJobsQueue, QueuePollTimeout).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // No jobs available
		}
		return nil, fmt.Errorf("failed to dequeue job: %w", err)
	}
	
	var job DownloadJob
	if err := json.Unmarshal([]byte(result), &job); err != nil {
		// If we can't unmarshal, move to failed queue
		qm.client.LPush(ctx, FailedJobsQueue, result)
		return nil, fmt.Errorf("failed to unmarshal job: %w", err)
	}
	
	// Update job with worker info
	job.StartedAt = time.Now()
	job.WorkerID = workerID
	
	// Update status to processing
	status := &JobStatus{
		ID:        job.ID,
		Status:    "processing",
		CreatedAt: job.CreatedAt,
		StartedAt: job.StartedAt,
		WorkerID:  workerID,
	}
	
	if err := qm.SetJobStatus(ctx, status); err != nil {
		qm.logger.Warn("Failed to set processing job status", 
			zap.String("job_id", job.ID),
			zap.String("worker_id", workerID),
			zap.Error(err))
	}
	
	qm.logger.Info("Job dequeued for processing", 
		zap.String("job_id", job.ID),
		zap.String("worker_id", workerID),
		zap.String("url", job.URL))
	
	return &job, nil
}

// CompleteJob marks a job as completed and moves it to completed queue
func (qm *QueueManager) CompleteJob(ctx context.Context, jobID string, workerID string) error {
	// Remove from processing queue
	if err := qm.removeFromProcessingQueue(ctx, jobID); err != nil {
		qm.logger.Warn("Failed to remove job from processing queue", 
			zap.String("job_id", jobID),
			zap.Error(err))
	}
	
	// Update status
	status := &JobStatus{
		ID:          jobID,
		Status:      "completed",
		CompletedAt: time.Now(),
		WorkerID:    workerID,
		Progress:    100.0,
	}
	
	if err := qm.SetJobStatus(ctx, status); err != nil {
		return fmt.Errorf("failed to set completed status: %w", err)
	}
	
	qm.logger.Info("Job completed successfully", 
		zap.String("job_id", jobID),
		zap.String("worker_id", workerID))
	
	return nil
}

// FailJob marks a job as failed and moves it to failed queue
func (qm *QueueManager) FailJob(ctx context.Context, jobID string, workerID string, errorMsg string) error {
	// Remove from processing queue
	if err := qm.removeFromProcessingQueue(ctx, jobID); err != nil {
		qm.logger.Warn("Failed to remove job from processing queue", 
			zap.String("job_id", jobID),
			zap.Error(err))
	}
	
	// Update status
	status := &JobStatus{
		ID:           jobID,
		Status:       "failed",
		CompletedAt:  time.Now(),
		WorkerID:     workerID,
		ErrorMessage: errorMsg,
	}
	
	if err := qm.SetJobStatus(ctx, status); err != nil {
		return fmt.Errorf("failed to set failed status: %w", err)
	}
	
	qm.logger.Error("Job failed", 
		zap.String("job_id", jobID),
		zap.String("worker_id", workerID),
		zap.String("error", errorMsg))
	
	return nil
}

// UpdateJobProgress updates the progress of a job
func (qm *QueueManager) UpdateJobProgress(ctx context.Context, jobID string, progress float64, bytesDownloaded, totalBytes int64) error {
	statusKey := fmt.Sprintf("job_status:%s", jobID)
	
	// Get current status
	statusData, err := qm.client.Get(ctx, statusKey).Result()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("failed to get current status: %w", err)
	}
	
	var status JobStatus
	if err == redis.Nil {
		// Status doesn't exist, create a basic one
		status = JobStatus{
			ID:     jobID,
			Status: "processing",
		}
	} else {
		if err := json.Unmarshal([]byte(statusData), &status); err != nil {
			return fmt.Errorf("failed to unmarshal status: %w", err)
		}
	}
	
	// Update progress fields
	status.Progress = progress
	status.BytesDownloaded = bytesDownloaded
	status.TotalBytes = totalBytes
	
	return qm.SetJobStatus(ctx, &status)
}

// SetJobStatus sets the status of a job
func (qm *QueueManager) SetJobStatus(ctx context.Context, status *JobStatus) error {
	statusKey := fmt.Sprintf("job_status:%s", status.ID)
	
	statusData, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal status: %w", err)
	}
	
	// Set status with expiration (30 days)
	if err := qm.client.Set(ctx, statusKey, statusData, 30*24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to set job status: %w", err)
	}
	
	return nil
}

// GetJobStatus retrieves the status of a job
func (qm *QueueManager) GetJobStatus(ctx context.Context, jobID string) (*JobStatus, error) {
	statusKey := fmt.Sprintf("job_status:%s", jobID)
	
	statusData, err := qm.client.Get(ctx, statusKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("job not found")
		}
		return nil, fmt.Errorf("failed to get job status: %w", err)
	}
	
	var status JobStatus
	if err := json.Unmarshal([]byte(statusData), &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal status: %w", err)
	}
	
	return &status, nil
}

// GetQueueStats returns statistics about the queues
func (qm *QueueManager) GetQueueStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)
	
	// Get queue lengths
	queuedJobs, err := qm.client.LLen(ctx, DownloadJobsQueue).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get queued jobs count: %w", err)
	}
	stats["queued"] = queuedJobs
	
	processingJobs, err := qm.client.LLen(ctx, ProcessingJobsQueue).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get processing jobs count: %w", err)
	}
	stats["processing"] = processingJobs
	
	completedJobs, err := qm.client.LLen(ctx, CompletedJobsQueue).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get completed jobs count: %w", err)
	}
	stats["completed"] = completedJobs
	
	failedJobs, err := qm.client.LLen(ctx, FailedJobsQueue).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get failed jobs count: %w", err)
	}
	stats["failed"] = failedJobs
	
	stats["total"] = queuedJobs + processingJobs + completedJobs + failedJobs
	
	return stats, nil
}

// removeFromProcessingQueue removes a job from the processing queue by job ID
func (qm *QueueManager) removeFromProcessingQueue(ctx context.Context, jobID string) error {
	// Get all jobs in processing queue
	jobs, err := qm.client.LRange(ctx, ProcessingJobsQueue, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to get processing jobs: %w", err)
	}
	
	// Find and remove the job
	for _, jobData := range jobs {
		var job DownloadJob
		if err := json.Unmarshal([]byte(jobData), &job); err != nil {
			continue
		}
		
		if job.ID == jobID {
			// Remove this specific job
			if err := qm.client.LRem(ctx, ProcessingJobsQueue, 1, jobData).Err(); err != nil {
				return fmt.Errorf("failed to remove job from processing queue: %w", err)
			}
			return nil
		}
	}
	
	return fmt.Errorf("job not found in processing queue")
}

// CleanupStaleJobs removes jobs that have been processing for too long
func (qm *QueueManager) CleanupStaleJobs(ctx context.Context) error {
	jobs, err := qm.client.LRange(ctx, ProcessingJobsQueue, 0, -1).Result()
	if err != nil {
		return fmt.Errorf("failed to get processing jobs: %w", err)
	}
	
	staleCount := 0
	for _, jobData := range jobs {
		var job DownloadJob
		if err := json.Unmarshal([]byte(jobData), &job); err != nil {
			continue
		}
		
		// Check if job is stale
		if time.Since(job.StartedAt) > JobProcessingTimeout {
			// Move back to main queue for retry
			if err := qm.client.LRem(ctx, ProcessingJobsQueue, 1, jobData).Err(); err != nil {
				qm.logger.Warn("Failed to remove stale job", zap.String("job_id", job.ID), zap.Error(err))
				continue
			}
			
			// Reset job timing
			job.StartedAt = time.Time{}
			job.WorkerID = ""
			
			jobDataReset, err := json.Marshal(job)
			if err != nil {
				qm.logger.Warn("Failed to marshal reset job", zap.String("job_id", job.ID), zap.Error(err))
				continue
			}
			
			if err := qm.client.LPush(ctx, DownloadJobsQueue, jobDataReset).Err(); err != nil {
				qm.logger.Warn("Failed to requeue stale job", zap.String("job_id", job.ID), zap.Error(err))
				continue
			}
			
			// Update status back to queued
			status := &JobStatus{
				ID:        job.ID,
				Status:    "queued",
				CreatedAt: job.CreatedAt,
			}
			qm.SetJobStatus(ctx, status)
			
			staleCount++
			qm.logger.Info("Requeued stale job", zap.String("job_id", job.ID))
		}
	}
	
	if staleCount > 0 {
		qm.logger.Info("Cleaned up stale jobs", zap.Int("count", staleCount))
	}
	
	return nil
}

// Close closes the Redis connection
func (qm *QueueManager) Close() error {
	return qm.client.Close()
}
