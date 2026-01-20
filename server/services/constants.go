package services

import (
	"os"
	"path/filepath"
)

// Shared constants and variables used across multiple service files

var (
	// MovieExtensions defines valid video file extensions
	MovieExtensions = map[string]bool{
		".mp4":  true,
		".mkv":  true,
		".avi":  true,
		".mov":  true,
		".wmv":  true,
		".m4v":  true,
		".flv":  true,
		".webm": true,
	}

	// PosterExtensions defines valid image file extensions for posters
	PosterExtensions = []string{".jpg", ".jpeg", ".png", ".webp"}

	// PosterNames defines common poster filename patterns
	PosterNames = []string{"poster", "folder", "cover", "movie", "show"}
)

const (
	// DefaultWorkerCount is the default number of workers for scanning operations
	DefaultWorkerCount = 4

	// TaskChannelBufferSize is the buffer size for task channels
	TaskChannelBufferSize = 100
)

// findLocalPoster searches for a local poster image in the given directory
// Returns the path to the poster if found, empty string otherwise
func findLocalPoster(dirPath string) string {
	for _, name := range PosterNames {
		for _, ext := range PosterExtensions {
			p := filepath.Join(dirPath, name+ext)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}
