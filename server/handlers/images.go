package handlers

import (
	"Arrgo/database"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func EnsureImageDir(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0755)
	}
	return nil
}

func downloadImage(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download image: %s", resp.Status)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func ServeMovieImage(w http.ResponseWriter, r *http.Request) {
	// URL: /images/movie/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	idStr := parts[3]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// 1. Look up movie in DB to get poster path
	var posterPath sql.NullString
	err = database.DB.QueryRow("SELECT poster_path FROM movies WHERE id = $1", id).Scan(&posterPath)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Movie not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	if !posterPath.Valid || posterPath.String == "" {
		// Serve default/placeholder or 404
		http.Error(w, "No poster available", http.StatusNotFound)
		return
	}

	// Determine file extension
	ext := filepath.Ext(posterPath.String)
	if ext == "" {
		ext = ".jpg"
	}
	
	// Local path: data/images/movies/{id}{ext}
	localDir := "data/images/movies"
	if err := EnsureImageDir(localDir); err != nil {
		log.Printf("Error creating image dir: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	
	localPath := filepath.Join(localDir, fmt.Sprintf("%d%s", id, ext))

	// 2. Check if file exists locally in cache
	if _, err := os.Stat(localPath); err == nil {
		http.ServeFile(w, r, localPath)
		return
	}

	// 2.5 Check if the poster_path itself is a local file (from scanner)
	// Only check if it's a full absolute path (contains more than just a leading slash)
	if strings.HasPrefix(posterPath.String, "/") && len(posterPath.String) > 1 && !strings.HasPrefix(posterPath.String, "//") {
		// Check if it's a real absolute path (like /mnt/movies/...)
		if strings.Contains(posterPath.String[1:], "/") {
			if _, err := os.Stat(posterPath.String); err == nil {
				http.ServeFile(w, r, posterPath.String)
				return
			}
		}
	}

	// 3. Not found locally, download it
	var sourceURL string
	pp := posterPath.String
	if strings.HasPrefix(pp, "http") {
		sourceURL = pp
	} else {
		// Remove leading slash - TMDB paths like "/9b0Im7SfedHiajTwzSL9zGyBI7M.jpg" are relative
		cleanPP := strings.TrimPrefix(pp, "/")
		sourceURL = fmt.Sprintf("https://image.tmdb.org/t/p/w500/%s", cleanPP)
	}

	log.Printf("[IMAGE] Downloading movie poster for ID %d from %s", id, sourceURL)
	if err := downloadImage(sourceURL, localPath); err != nil {
		log.Printf("[IMAGE] Failed to download: %v", err)
		http.Error(w, "Failed to fetch image", http.StatusBadGateway)
		return
	}

	http.ServeFile(w, r, localPath)
}

func ServeShowImage(w http.ResponseWriter, r *http.Request) {
	// URL: /images/shows/{id}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	idStr := parts[3]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid ID", http.StatusBadRequest)
		return
	}

	// 1. Look up show in DB to get poster path
	var posterPath sql.NullString
	err = database.DB.QueryRow("SELECT poster_path FROM shows WHERE id = $1", id).Scan(&posterPath)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Show not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	if !posterPath.Valid || posterPath.String == "" {
		http.Error(w, "No poster available", http.StatusNotFound)
		return
	}

	ext := filepath.Ext(posterPath.String)
	if ext == "" {
		ext = ".jpg"
	}

	// Local path: data/images/shows/{id}{ext}
	localDir := "data/images/shows"
	if err := EnsureImageDir(localDir); err != nil {
		log.Printf("Error creating image dir: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	localPath := filepath.Join(localDir, fmt.Sprintf("%d%s", id, ext))

	if _, err := os.Stat(localPath); err == nil {
		http.ServeFile(w, r, localPath)
		return
	}

	// Check if the poster_path itself is a local file (from scanner)
	// Only check if it's a full absolute path (contains more than just a leading slash)
	if strings.HasPrefix(posterPath.String, "/") && len(posterPath.String) > 1 && !strings.HasPrefix(posterPath.String, "//") {
		// Check if it's a real absolute path (like /mnt/shows/...)
		if strings.Contains(posterPath.String[1:], "/") {
			if _, err := os.Stat(posterPath.String); err == nil {
				http.ServeFile(w, r, posterPath.String)
				return
			}
		}
	}

	var sourceURL string
	pp := posterPath.String
	if strings.HasPrefix(pp, "http") {
		sourceURL = pp
	} else {
		// Remove leading slash - TMDB paths like "/9b0Im7SfedHiajTwzSL9zGyBI7M.jpg" are relative
		cleanPP := strings.TrimPrefix(pp, "/")
		sourceURL = fmt.Sprintf("https://image.tmdb.org/t/p/w500/%s", cleanPP)
	}

	log.Printf("[IMAGE] Downloading show poster for ID %d from %s", id, sourceURL)
	if err := downloadImage(sourceURL, localPath); err != nil {
		log.Printf("[IMAGE] Failed to download: %v", err)
		http.Error(w, "Failed to fetch image", http.StatusBadGateway)
		return
	}

	http.ServeFile(w, r, localPath)
}

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
	} else if strings.HasPrefix(path, "/") {
		// This should have been caught by step 1 if the file existed.
		// If it reaches here, it's a local path that doesn't exist.
		// We should NOT try to download it from TMDB.
		http.Error(w, "Local image not found", http.StatusNotFound)
		return
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
