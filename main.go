package main

import (
	"flag"
	"fmt"
	"os"

	"multithreaded-downloader/downloader"
)

func main() {
	// Define command-line flags
	var (
		url        = flag.String("url", "", "URL to download")
		output     = flag.String("output", "", "Output filename")
		threads    = flag.Int("threads", 4, "Number of download threads")
		showHelp   = flag.Bool("help", false, "Show help message")
	)

	// Custom usage function
	flag.Usage = func() {
		fmt.Println("Multithreaded Downloader v1.0")
		fmt.Println("═══════════════════════════════")
		fmt.Println()
		fmt.Println("Usage:")
		fmt.Printf("  %s --url <URL> --output <filename> [--threads <number>]\n", os.Args[0])
		fmt.Println()
		fmt.Println("Flags:")
		fmt.Println("  --url string       URL to download (required)")
		fmt.Println("  --output string    Output filename (required)")
		fmt.Println("  --threads int      Number of download threads (default 4)")
		fmt.Println("  --help             Show this help message")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Printf("  %s --url https://example.com/file.zip --output download.zip\n", os.Args[0])
		fmt.Printf("  %s --url https://example.com/file.zip --output download.zip --threads 8\n", os.Args[0])
		fmt.Println()
		fmt.Println("Features:")
		fmt.Println("- Multithreaded downloading with configurable thread count")
		fmt.Println("- Resume support with progress tracking")
		fmt.Println("- Real-time progress display")
		fmt.Println("- Automatic HTTP range support detection")
		fmt.Println("- Progress saved as download_state.json")
	}

	// Parse command-line flags
	flag.Parse()

	// Show help if requested or if no arguments provided
	if *showHelp || len(os.Args) == 1 {
		flag.Usage()
		os.Exit(0)
	}

	// Validate required flags
	if *url == "" || *output == "" {
		fmt.Println("Error: Both --url and --output are required")
		fmt.Println()
		flag.Usage()
		os.Exit(1)
	}

	// Validate threads count
	if *threads < 1 {
		fmt.Println("Error: Number of threads must be at least 1")
		os.Exit(1)
	}

	fmt.Println("Multithreaded Downloader v1.0")
	fmt.Println("═══════════════════════════════")

	// Create downloader instance
	dl := downloader.NewDownloader(*url, *output, *threads)

	// Load or create progress
	if err := dl.LoadOrCreateProgress(); err != nil {
		fmt.Printf("Error initializing download: %v\n", err)
		os.Exit(1)
	}

	// Start the download
	if err := dl.Download(); err != nil {
		fmt.Printf("Error during download: %v\n", err)
		os.Exit(1)
	}

	// Verify download completion
	if err := dl.VerifyDownload(); err != nil {
		fmt.Printf("⚠️  %v\n", err)
		fmt.Println("Run the same command again to resume the download.")
		os.Exit(1)
	}
}