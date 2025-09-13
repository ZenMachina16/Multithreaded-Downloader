package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Example client demonstrating how to use the REST API
func main() {
	baseURL := "http://localhost:8080"
	
	fmt.Println("Multithreaded Downloader API Client Example")
	fmt.Println("===========================================")
	
	// 1. Start a download
	fmt.Println("\n1. Starting a new download...")
	downloadReq := map[string]interface{}{
		"url":     "https://httpbin.org/bytes/5242880", // 5MB test file
		"output":  "example_download.bin",
		"threads": 4,
	}
	
	reqBody, _ := json.Marshal(downloadReq)
	resp, err := http.Post(baseURL+"/downloads", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Printf("Error starting download: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	var downloadResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&downloadResp); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		return
	}
	
	downloadID := downloadResp["download_id"].(string)
	fmt.Printf("Download started with ID: %s\n", downloadID)
	
	// 2. Monitor progress
	fmt.Println("\n2. Monitoring download progress...")
	for i := 0; i < 20; i++ {
		resp, err := http.Get(fmt.Sprintf("%s/downloads/%s/status", baseURL, downloadID))
		if err != nil {
			fmt.Printf("Error checking status: %v\n", err)
			break
		}
		
		var status map[string]interface{}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		
		if err := json.Unmarshal(body, &status); err != nil {
			fmt.Printf("Error parsing status: %v\n", err)
			break
		}
		
		percent := status["percent_completed"].(float64)
		downloadStatus := status["status"].(string)
		
		fmt.Printf("Progress: %.2f%% - Status: %s\n", percent, downloadStatus)
		
		if downloadStatus == "completed" || downloadStatus == "failed" {
			break
		}
		
		time.Sleep(1 * time.Second)
	}
	
	// 3. Check final status
	fmt.Println("\n3. Final status check...")
	resp, err = http.Get(fmt.Sprintf("%s/downloads/%s/status", baseURL, downloadID))
	if err != nil {
		fmt.Printf("Error checking final status: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Final status: %s\n", string(body))
	
	// 4. List all downloads
	fmt.Println("\n4. Listing all downloads...")
	resp, err = http.Get(baseURL + "/downloads")
	if err != nil {
		fmt.Printf("Error listing downloads: %v\n", err)
		return
	}
	defer resp.Body.Close()
	
	body, _ = io.ReadAll(resp.Body)
	fmt.Printf("All downloads: %s\n", string(body))
	
	fmt.Println("\nExample completed!")
}
