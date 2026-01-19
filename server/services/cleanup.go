package services

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func CleanupEmptyDirs(root string) error {
	if root == "" {
		return nil
	}

	// 1. Collect all directories
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 2. Sort by length descending to process children before parents
	sort.Slice(dirs, func(i, j int) bool {
		return len(dirs[i]) > len(dirs[j])
	})

	// List of junk files that shouldn't prevent a folder from being deleted in incoming
	junkExtensions := map[string]bool{
		".nfo": true, ".txt": true, ".url": true, ".exe": true,
		".db":  true, ".md":  true, ".png": true, ".jpg": true,
		".jpeg": true, ".gif": true, ".sfv": true, ".srr": true,
		".xml": true, ".html": true, ".htm": true, ".info": true,
		".srt": true, ".sub": true, ".idx": true, ".m3u": true,
		".m3u8": true, ".parts": true, ".sample": true,
		".tbn": true, ".ico": true, ".desktop": true, ".ini": true,
		".ds_store": true, "thumbs.db": true, ".torrent": true,
	}

	for _, path := range dirs {
		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}

		actuallyEmpty := true
		for _, entry := range entries {
			if entry.IsDir() {
				// Since we process children first, if a subdir still exists,
				// it means it wasn't empty/junk-only.
				actuallyEmpty = false
				break
			}

			name := strings.ToLower(entry.Name())
			ext := filepath.Ext(name)
			
			// Also check for "sample" in filename
			isJunk := junkExtensions[ext] || strings.Contains(name, "sample")
			
			if !isJunk {
				actuallyEmpty = false
				break
			}
		}

		if actuallyEmpty {
			log.Printf("[CLEANUP] Removing directory (contains only junk or empty): %s", path)
			// Use RemoveAll to be sure, though Remove should work if we only have junk files
			err := os.RemoveAll(path)
			if err != nil {
				log.Printf("[CLEANUP] Failed to remove %s: %v", path, err)
			}
		}
	}

	return nil
}

func IsDirEmpty(name string) (bool, error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if err == io.EOF {
		return true, nil
	}
	return false, err
}
