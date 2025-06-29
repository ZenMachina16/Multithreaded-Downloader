package downloader

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Downloader handles the multithreaded download process
type Downloader struct {
	URL         string
	Filename    string
	NumThreads  int
	ProgressFile string
	Progress    *Progress
}

// NewDownloader creates a new downloader instance
func NewDownloader(url, filename string, numThreads int) *Downloader {
	return &Downloader{
		URL:          url,
		Filename:     filename,
		NumThreads:   numThreads,
		ProgressFile: "download_state.json",
	}
}

// SupportsRange checks if the server supports HTTP range requests
func (d *Downloader) SupportsRange() (bool, int64, error) {
	fmt.Printf("Checking if server supports range requests for: %s\n", d.URL)
	
	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}

	var supportsRanges bool
	var length int64

	// First try HEAD request
	resp, err := client.Head(d.URL)
	if err != nil {
		fmt.Printf("HEAD request failed (%v), trying GET request...\n", err)
		
		// Fallback: Try a small range GET request to test range support
		req, err := http.NewRequest("GET", d.URL, nil)
		if err != nil {
			return false, 0, fmt.Errorf("failed to create GET request: %w", err)
		}
		req.Header.Set("Range", "bytes=0-1023") // Request first 1KB
		req.Header.Set("User-Agent", "Go-Downloader/1.0")
		
		resp, err = client.Do(req)
		if err != nil {
			return false, 0, fmt.Errorf("failed to make GET request: %w", err)
		}
		defer resp.Body.Close()

		// Check if we got partial content (range support)
		if resp.StatusCode == http.StatusPartialContent {
			supportsRanges = true
			// Parse Content-Range to get total size
			contentRange := resp.Header.Get("Content-Range")
			if contentRange != "" {
				fmt.Printf("Content-Range: %s\n", contentRange)
				var start, end, total int64
				if n, _ := fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total); n == 3 {
					length = total
				}
			}
		} else if resp.StatusCode == http.StatusOK {
			// Server doesn't support ranges, but we can still download
			supportsRanges = false
			length = resp.ContentLength
		} else {
			return false, 0, fmt.Errorf("server returned status: %s", resp.Status)
		}

		// If we still don't have the length, make a full HEAD/GET request
		if length <= 0 {
			fmt.Println("Getting file size with full request...")
			fullResp, err := client.Get(d.URL)
			if err != nil {
				return false, 0, fmt.Errorf("failed to get file size: %w", err)
			}
			defer fullResp.Body.Close()
			
			if fullResp.StatusCode == http.StatusOK {
				length = fullResp.ContentLength
			}
		}
	} else {
		// HEAD request succeeded
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			return false, 0, fmt.Errorf("server returned status: %s", resp.Status)
		}

		length = resp.ContentLength
		supportsRanges = resp.Header.Get("Accept-Ranges") == "bytes"
	}

	if length <= 0 {
		return false, 0, fmt.Errorf("server did not provide content length")
	}

	fmt.Printf("Server supports range requests: %v\n", supportsRanges)
	fmt.Printf("File size: %d bytes (%.2f MB)\n", length, float64(length)/(1024*1024))

	return supportsRanges, length, nil
}

// LoadOrCreateProgress loads existing progress or creates new one
func (d *Downloader) LoadOrCreateProgress() error {
	// Try to load existing progress
	if existingProgress, err := LoadProgress(d.ProgressFile); err == nil {
		if existingProgress.URL == d.URL && existingProgress.Filename == d.Filename {
			fmt.Println("Found existing download progress. Resuming...")
			d.Progress = existingProgress
			return nil
		} else {
			fmt.Println("Previous download was for different URL/file. Starting new download...")
		}
	}

	// Create new progress
	supportsRanges, totalSize, err := d.SupportsRange()
	if err != nil {
		return fmt.Errorf("error checking server capabilities: %w", err)
	}

	if !supportsRanges {
		fmt.Println("Server does not support range requests. Falling back to single-threaded download...")
		d.NumThreads = 1
	}

	d.Progress = CreateNewProgress(d.URL, d.Filename, totalSize, d.NumThreads)
	return SaveProgress(d.ProgressFile, d.Progress)
}

// PrintProgress displays the current download progress
func (d *Downloader) PrintProgress() {
	fmt.Print("\033[H\033[2J") // Clear screen
	fmt.Printf("Downloading: %s\n", d.Progress.URL)
	fmt.Printf("Output file: %s\n", d.Progress.Filename)
	fmt.Printf("Total size: %.2f MB\n\n", float64(d.Progress.TotalSize)/(1024*1024))

	totalDownloaded := d.Progress.GetTotalDownloaded()
	overallPercent := d.Progress.GetOverallPercent()

	fmt.Printf("Overall Progress: %.2f%% (%.2f MB / %.2f MB)\n", 
		overallPercent, 
		float64(totalDownloaded)/(1024*1024), 
		float64(d.Progress.TotalSize)/(1024*1024))
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	for _, part := range d.Progress.Parts {
		partSize := part.End - part.Start + 1
		percent := float64(part.Downloaded) / float64(partSize) * 100
		
		barLength := 40
		filled := int(percent * float64(barLength) / 100)
		
		bar := ""
		for i := 0; i < barLength; i++ {
			if i < filled {
				bar += "█"
			} else {
				bar += "░"
			}
		}

		status := "Downloading"
		if part.Done {
			status = "Complete"
		}

		fmt.Printf("Part %d: [%s] %6.2f%% (%s)\n", 
			part.Index+1, bar, percent, status)
	}
}

// downloadPart downloads a specific part of the file
func (d *Downloader) downloadPart(ctx context.Context, part *Part, progressMutex *sync.Mutex, wg *sync.WaitGroup) {
	defer wg.Done()

	if part.Done {
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Calculate current position
		currentStart := part.Start + part.Downloaded
		if currentStart > part.End {
			part.Done = true
			return
		}

		// Create request with range header
		req, err := http.NewRequestWithContext(ctx, "GET", d.URL, nil)
		if err != nil {
			fmt.Printf("Error creating request for part %d: %v\n", part.Index, err)
			time.Sleep(time.Second)
			continue
		}

		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", currentStart, part.End))
		req.Header.Set("User-Agent", "Go-Downloader/1.0")

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("Error downloading part %d: %v\n", part.Index, err)
			time.Sleep(time.Second)
			continue
		}

		if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			fmt.Printf("Unexpected status for part %d: %s\n", part.Index, resp.Status)
			time.Sleep(time.Second)
			continue
		}

		// Open file for writing
		file, err := os.OpenFile(d.Filename, os.O_WRONLY|os.O_CREATE, 0644)
		if err != nil {
			resp.Body.Close()
			fmt.Printf("Error opening file for part %d: %v\n", part.Index, err)
			time.Sleep(time.Second)
			continue
		}

		// Seek to correct position
		_, err = file.Seek(currentStart, 0)
		if err != nil {
			file.Close()
			resp.Body.Close()
			fmt.Printf("Error seeking in file for part %d: %v\n", part.Index, err)
			time.Sleep(time.Second)
			continue
		}

		// Download with progress tracking
		buffer := make([]byte, 32*1024) // 32KB buffer
		for {
			select {
			case <-ctx.Done():
				file.Close()
				resp.Body.Close()
				return
			default:
			}

			n, err := resp.Body.Read(buffer)
			if n > 0 {
				written, writeErr := file.Write(buffer[:n])
				if writeErr != nil {
					fmt.Printf("Error writing to file for part %d: %v\n", part.Index, writeErr)
					break
				}
				atomic.AddInt64(&part.Downloaded, int64(written))
			}

			if err != nil {
				if err == io.EOF {
					// Download completed successfully
					part.Done = true
				}
				break
			}
		}

		file.Close()
		resp.Body.Close()

		if part.Done || part.Downloaded >= (part.End-part.Start+1) {
			part.Done = true
			break
		}
	}
}

// Download starts the multithreaded download process
func (d *Downloader) Download() error {
	// Create context for cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create the output file if it doesn't exist
	if _, err := os.Stat(d.Filename); os.IsNotExist(err) {
		file, err := os.Create(d.Filename)
		if err != nil {
			return fmt.Errorf("error creating output file: %w", err)
		}
		file.Close()
	}

	// Start progress display goroutine
	progressMutex := &sync.Mutex{}
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				progressMutex.Lock()
				d.PrintProgress()
				// Save progress periodically
				SaveProgress(d.ProgressFile, d.Progress)
				progressMutex.Unlock()
			}
		}
	}()

	// Start download goroutines
	var wg sync.WaitGroup
	fmt.Printf("Starting download with %d threads...\n", d.Progress.NumThreads)
	
	for i := range d.Progress.Parts {
		if !d.Progress.Parts[i].Done {
			wg.Add(1)
			go d.downloadPart(ctx, &d.Progress.Parts[i], progressMutex, &wg)
		}
	}

	// Wait for all downloads to complete
	wg.Wait()
	cancel() // Stop progress display

	// Final progress save
	SaveProgress(d.ProgressFile, d.Progress)

	return nil
}

// VerifyDownload checks if the download completed successfully
func (d *Downloader) VerifyDownload() error {
	if d.Progress.IsComplete() {
		fmt.Printf("\n✅ Download completed successfully!\n")
		fmt.Printf("File saved as: %s\n", d.Progress.Filename)
		
		// Verify file size
		if stat, err := os.Stat(d.Progress.Filename); err == nil {
			if stat.Size() == d.Progress.TotalSize {
				fmt.Printf("File size verified: %d bytes\n", stat.Size())
				// Clean up progress file on successful completion
				os.Remove(d.ProgressFile)
				return nil
			} else {
				return fmt.Errorf("file size mismatch! Expected: %d, Got: %d", d.Progress.TotalSize, stat.Size())
			}
		}
	} else {
		return fmt.Errorf("download incomplete. Progress saved to %s", d.ProgressFile)
	}
	return nil
} 