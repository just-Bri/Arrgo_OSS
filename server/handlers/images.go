package handlers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func ImageProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Format: /images/tmdb/{path_or_id}
	// We handle both /images/tmdb/path and /images/tmdb//path (extra slash)
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/images/tmdb")
	path = strings.TrimPrefix(path, "/")

	// 1. Check if it's an absolute local path (from scanner)
	if strings.HasPrefix(path, "/") {
		if _, err := os.Stat(path); err == nil {
			http.ServeFile(w, r, path)
			return
		}
	}

	// 2. Check local cache in data/posters/
	cacheDir := "data/posters"
	os.MkdirAll(cacheDir, 0755)

	// Create a safe filename from the path/URL
	safeName := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			return r
		}
		return '_'
	}, path)
	cachePath := filepath.Join(cacheDir, safeName)

	if _, err := os.Stat(cachePath); err == nil {
		http.ServeFile(w, r, cachePath)
		return
	}

	// 3. Not in cache, determine source
	var sourceURL string
	if strings.HasPrefix(path, "http") {
		// It's a full URL (likely TVDB)
		sourceURL = path
	} else {
		// It's a TMDB relative path
		sourceURL = fmt.Sprintf("https://image.tmdb.org/t/p/w500/%s", path)
	}

	log.Printf("[IMAGE] Downloading poster from %s to %s", sourceURL, cachePath)
	resp, err := http.Get(sourceURL)
	if err != nil {
		http.Error(w, "Failed to fetch image", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Image not found", resp.StatusCode)
		return
	}

	// Create cache file
	out, err := os.Create(cachePath)
	if err != nil {
		// If we can't create cache, just proxy it
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
		return
	}
	defer out.Close()

	// Write to both file and response
	mw := io.MultiWriter(w, out)
	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(mw, resp.Body)
}
