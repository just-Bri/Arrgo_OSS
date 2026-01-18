package services

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

func CleanupEmptyDirs(root string) error {
	if root == "" {
		return nil
	}

	// We use a post-order traversal (children before parents) to ensure
	// that if a directory becomes empty after its children are deleted,
	// it can also be deleted in the same pass.
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}
		if !d.IsDir() || path == root {
			return nil
		}

		// Recurse first
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}

		// List of junk files that shouldn't prevent a folder from being deleted in incoming
		junkExtensions := map[string]bool{
			".nfo": true, ".txt": true, ".url": true, ".exe": true,
			".db":  true, ".md":  true, ".png": true, ".jpg": true,
			".jpeg": true, ".gif": true, ".sfv": true, ".srr": true,
		}

		actuallyEmpty := true
		for _, entry := range entries {
			if entry.IsDir() {
				// If there's a sub-directory, it's not empty yet
				// (It might be cleaned up later if WalkDir visits it,
				// but WalkDir is top-down).
				actuallyEmpty = false
				break
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if !junkExtensions[ext] {
				actuallyEmpty = false
				break
			}
		}

		if actuallyEmpty {
			log.Printf("[CLEANUP] Removing directory (contains only junk or empty): %s", path)
			return os.RemoveAll(path)
		}

		return nil
	})
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
