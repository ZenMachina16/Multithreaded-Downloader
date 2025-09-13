package main

import (
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Download represents a download record in the database
type Download struct {
	ID              string    `gorm:"primaryKey;type:text" json:"id"`
	URL             string    `gorm:"not null" json:"url"`
	OutputPath      string    `gorm:"not null" json:"output_path"`
	Threads         int       `gorm:"not null;default:4" json:"threads"`
	Status          string    `gorm:"not null;default:'downloading'" json:"status"`
	BytesDownloaded int64     `gorm:"default:0" json:"bytes_downloaded"`
	TotalBytes      int64     `gorm:"default:0" json:"total_bytes"`
	StartTime       time.Time `gorm:"not null" json:"start_time"`
	UpdatedAt       time.Time `gorm:"autoUpdateTime" json:"updated_at"`
	CreatedAt       time.Time `gorm:"autoCreateTime" json:"created_at"`
	Error           string    `gorm:"type:text" json:"error,omitempty"`
}

// DatabaseManager handles all database operations
type DatabaseManager struct {
	db *gorm.DB
}

var dbManager *DatabaseManager

// InitDatabase initializes the SQLite database connection and creates tables
func InitDatabase(dbPath string) error {
	// Configure GORM logger for production
	gormLogger := logger.New(
		log.New(log.Writer(), "\r\n", log.LstdFlags),
		logger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  logger.Error, // Only log errors in production
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	// Open database connection
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: gormLogger,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	// Configure SQLite connection pool
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Set connection pool settings
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// Auto-migrate the schema
	if err := db.AutoMigrate(&Download{}); err != nil {
		return fmt.Errorf("failed to migrate database schema: %w", err)
	}

	dbManager = &DatabaseManager{db: db}
	
	fmt.Println("Database initialized successfully")
	return nil
}

// GetDB returns the database instance
func GetDB() *gorm.DB {
	if dbManager == nil {
		log.Fatal("Database not initialized. Call InitDatabase first.")
	}
	return dbManager.db
}

// CreateDownload creates a new download record in the database
func (dm *DatabaseManager) CreateDownload(id, url, outputPath string, threads int) (*Download, error) {
	download := &Download{
		ID:         id,
		URL:        url,
		OutputPath: outputPath,
		Threads:    threads,
		Status:     "downloading",
		StartTime:  time.Now(),
	}

	if err := dm.db.Create(download).Error; err != nil {
		return nil, fmt.Errorf("failed to create download record: %w", err)
	}

	return download, nil
}

// UpdateDownloadProgress updates the progress of a download
func (dm *DatabaseManager) UpdateDownloadProgress(id string, bytesDownloaded, totalBytes int64, status string) error {
	updates := map[string]interface{}{
		"bytes_downloaded": bytesDownloaded,
		"total_bytes":      totalBytes,
		"status":           status,
		"updated_at":       time.Now(),
	}

	result := dm.db.Model(&Download{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update download progress: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("download with id %s not found", id)
	}

	return nil
}

// UpdateDownloadStatus updates the status and error message of a download
func (dm *DatabaseManager) UpdateDownloadStatus(id, status, errorMsg string) error {
	updates := map[string]interface{}{
		"status":     status,
		"updated_at": time.Now(),
	}

	if errorMsg != "" {
		updates["error"] = errorMsg
	}

	result := dm.db.Model(&Download{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("failed to update download status: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("download with id %s not found", id)
	}

	return nil
}

// GetDownload retrieves a download by ID
func (dm *DatabaseManager) GetDownload(id string) (*Download, error) {
	var download Download
	if err := dm.db.Where("id = ?", id).First(&download).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("download with id %s not found", id)
		}
		return nil, fmt.Errorf("failed to get download: %w", err)
	}
	return &download, nil
}

// GetAllDownloads retrieves all downloads
func (dm *DatabaseManager) GetAllDownloads() ([]Download, error) {
	var downloads []Download
	if err := dm.db.Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to get all downloads: %w", err)
	}
	return downloads, nil
}

// GetIncompleteDownloads retrieves downloads that are not completed or failed
func (dm *DatabaseManager) GetIncompleteDownloads() ([]Download, error) {
	var downloads []Download
	if err := dm.db.Where("status IN ?", []string{"downloading", "paused"}).Find(&downloads).Error; err != nil {
		return nil, fmt.Errorf("failed to get incomplete downloads: %w", err)
	}
	return downloads, nil
}

// DeleteDownload removes a download record from the database
func (dm *DatabaseManager) DeleteDownload(id string) error {
	result := dm.db.Where("id = ?", id).Delete(&Download{})
	if result.Error != nil {
		return fmt.Errorf("failed to delete download: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("download with id %s not found", id)
	}

	return nil
}

// CleanupCompletedDownloads removes completed downloads older than the specified duration
func (dm *DatabaseManager) CleanupCompletedDownloads(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	result := dm.db.Where("status = ? AND updated_at < ?", "completed", cutoff).Delete(&Download{})
	if result.Error != nil {
		return fmt.Errorf("failed to cleanup completed downloads: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		fmt.Printf("Cleaned up %d completed downloads older than %v\n", result.RowsAffected, olderThan)
	}

	return nil
}

// GetDownloadStats returns statistics about downloads
func (dm *DatabaseManager) GetDownloadStats() (map[string]int64, error) {
	stats := make(map[string]int64)

	// Count by status
	var results []struct {
		Status string
		Count  int64
	}

	if err := dm.db.Model(&Download{}).Select("status, COUNT(*) as count").Group("status").Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("failed to get download stats: %w", err)
	}

	for _, result := range results {
		stats[result.Status] = result.Count
	}

	// Total downloads
	var total int64
	if err := dm.db.Model(&Download{}).Count(&total).Error; err != nil {
		return nil, fmt.Errorf("failed to count total downloads: %w", err)
	}
	stats["total"] = total

	return stats, nil
}

// Close closes the database connection
func (dm *DatabaseManager) Close() error {
	if dm.db != nil {
		sqlDB, err := dm.db.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

// Helper functions for easier access

// SaveDownload creates or updates a download record
func SaveDownload(id, url, outputPath string, threads int) (*Download, error) {
	if dbManager == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return dbManager.CreateDownload(id, url, outputPath, threads)
}

// UpdateProgress updates download progress in the database
func UpdateProgress(id string, bytesDownloaded, totalBytes int64, status string) error {
	if dbManager == nil {
		return fmt.Errorf("database not initialized")
	}
	return dbManager.UpdateDownloadProgress(id, bytesDownloaded, totalBytes, status)
}

// UpdateStatus updates download status in the database
func UpdateStatus(id, status, errorMsg string) error {
	if dbManager == nil {
		return fmt.Errorf("database not initialized")
	}
	return dbManager.UpdateDownloadStatus(id, status, errorMsg)
}

// GetDownloadByID retrieves a download by ID
func GetDownloadByID(id string) (*Download, error) {
	if dbManager == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return dbManager.GetDownload(id)
}

// GetAllDownloadsFromDB retrieves all downloads from database
func GetAllDownloadsFromDB() ([]Download, error) {
	if dbManager == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return dbManager.GetAllDownloads()
}

// GetIncompleteDownloadsFromDB retrieves incomplete downloads for resuming
func GetIncompleteDownloadsFromDB() ([]Download, error) {
	if dbManager == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	return dbManager.GetIncompleteDownloads()
}

// RemoveDownload deletes a download from the database
func RemoveDownload(id string) error {
	if dbManager == nil {
		return fmt.Errorf("database not initialized")
	}
	return dbManager.DeleteDownload(id)
}
