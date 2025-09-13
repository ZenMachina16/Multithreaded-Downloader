// +build ignore

// Test file for persistence functionality
// This file demonstrates that the persistence logic is correct
// Run with: go run test_persistence.go db.go

package main

import (
	"fmt"
	"log"
	"time"
)

func main() {
	fmt.Println("Testing Database Persistence")
	fmt.Println("============================")
	
	// Initialize database
	if err := InitDatabase("test_downloads.db"); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer func() {
		if dbManager != nil {
			dbManager.Close()
		}
	}()
	
	// Test 1: Create a download record
	fmt.Println("\n1. Creating download record...")
	downloadID := "test-download-123"
	download, err := SaveDownload(downloadID, "https://example.com/test.zip", "test.zip", 4)
	if err != nil {
		log.Fatalf("Failed to create download: %v", err)
	}
	fmt.Printf("Created download: %+v\n", download)
	
	// Test 2: Update progress
	fmt.Println("\n2. Updating progress...")
	if err := UpdateProgress(downloadID, 1024, 10240, "downloading"); err != nil {
		log.Fatalf("Failed to update progress: %v", err)
	}
	fmt.Println("Progress updated successfully")
	
	// Test 3: Get download
	fmt.Println("\n3. Retrieving download...")
	retrieved, err := GetDownloadByID(downloadID)
	if err != nil {
		log.Fatalf("Failed to get download: %v", err)
	}
	fmt.Printf("Retrieved download: %+v\n", retrieved)
	
	// Test 4: Get incomplete downloads
	fmt.Println("\n4. Getting incomplete downloads...")
	incomplete, err := GetIncompleteDownloadsFromDB()
	if err != nil {
		log.Fatalf("Failed to get incomplete downloads: %v", err)
	}
	fmt.Printf("Found %d incomplete downloads\n", len(incomplete))
	
	// Test 5: Update status to completed
	fmt.Println("\n5. Marking as completed...")
	if err := UpdateStatus(downloadID, "completed", ""); err != nil {
		log.Fatalf("Failed to update status: %v", err)
	}
	fmt.Println("Status updated to completed")
	
	// Test 6: Get stats
	fmt.Println("\n6. Getting statistics...")
	if dbManager != nil {
		stats, err := dbManager.GetDownloadStats()
		if err != nil {
			log.Fatalf("Failed to get stats: %v", err)
		}
		fmt.Printf("Statistics: %+v\n", stats)
	}
	
	// Test 7: Clean up
	fmt.Println("\n7. Cleaning up...")
	if err := RemoveDownload(downloadID); err != nil {
		log.Fatalf("Failed to remove download: %v", err)
	}
	fmt.Println("Download removed successfully")
	
	fmt.Println("\n✅ All persistence tests passed!")
}
