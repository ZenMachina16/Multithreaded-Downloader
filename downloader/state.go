package downloader

import (
	"encoding/json"
	"os"
)

// Part represents a single download part/chunk
type Part struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
	Done       bool  `json:"done"`
}

// Progress represents the overall download state
type Progress struct {
	URL        string `json:"url"`
	Filename   string `json:"filename"`
	TotalSize  int64  `json:"total_size"`
	Parts      []Part `json:"parts"`
	NumThreads int    `json:"num_threads"`
}

// SaveProgress saves the current progress to a JSON file
func SaveProgress(filename string, progress *Progress) error {
	data, err := json.MarshalIndent(progress, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// LoadProgress loads progress from a JSON file
func LoadProgress(filename string) (*Progress, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	
	var progress Progress
	err = json.Unmarshal(data, &progress)
	return &progress, err
}

// CreateNewProgress creates a new progress structure for a fresh download
func CreateNewProgress(url, filename string, totalSize int64, numThreads int) *Progress {
	partSize := totalSize / int64(numThreads)
	parts := make([]Part, numThreads)

	for i := 0; i < numThreads; i++ {
		start := int64(i) * partSize
		end := start + partSize - 1
		if i == numThreads-1 {
			end = totalSize - 1
		}

		parts[i] = Part{
			Index:      i,
			Start:      start,
			End:        end,
			Downloaded: 0,
			Done:       false,
		}
	}

	return &Progress{
		URL:        url,
		Filename:   filename,
		TotalSize:  totalSize,
		Parts:      parts,
		NumThreads: numThreads,
	}
}

// IsComplete checks if all parts are downloaded
func (p *Progress) IsComplete() bool {
	for _, part := range p.Parts {
		if !part.Done {
			return false
		}
	}
	return true
}

// GetTotalDownloaded returns the total bytes downloaded across all parts
func (p *Progress) GetTotalDownloaded() int64 {
	var total int64
	for _, part := range p.Parts {
		total += part.Downloaded
	}
	return total
}

// GetOverallPercent returns the overall download percentage
func (p *Progress) GetOverallPercent() float64 {
	if p.TotalSize == 0 {
		return 0
	}
	return float64(p.GetTotalDownloaded()) / float64(p.TotalSize) * 100
} 