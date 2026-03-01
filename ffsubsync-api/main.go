package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync" // Added for mutex
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type SyncRequest struct {
	Video    string `json:"video"`
	Subtitle string `json:"subtitle"`
}

var (
	// syncMutex ensures only one ffsubsync process runs at a time to prevent CPU exhaustion.
	syncMutex sync.Mutex

	moviesPath string
	showsPath  string
)

func isPathAllowed(path string) bool {
	// If paths aren't set, allow all (backward compatibility/default behavior)
	if moviesPath == "" && showsPath == "" {
		return true
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	if moviesPath != "" && strings.HasPrefix(absPath, moviesPath) {
		return true
	}
	if showsPath != "" && strings.HasPrefix(absPath, showsPath) {
		return true
	}

	return false
}

func main() {
	moviesPath = os.Getenv("MOVIES_PATH")
	showsPath = os.Getenv("SHOWS_PATH")

	log.Printf("SubSync API starting...")
	log.Printf("Allowed Movies Path: %s", moviesPath)
	log.Printf("Allowed Shows Path: %s", showsPath)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.CleanPath)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("REQUEST: %s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
			next.ServeHTTP(w, r)
		})
	})
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Minute))
	r.Use(middleware.Compress(5))

	r.Post("/sync", func(w http.ResponseWriter, r *http.Request) {
		req := new(SyncRequest)
		if err := json.NewDecoder(r.Body).Decode(req); err != nil {
			http.Error(w, "Invalid JSON format", http.StatusBadRequest)
			return
		}

		if req.Video == "" || req.Subtitle == "" {
			http.Error(w, "Missing video or subtitle path", http.StatusBadRequest)
			return
		}

		// Use the absolute path provided in the request as they are mapped in the same way
		// between Arrgo and this container via the shared media volume.
		videoPath := req.Video
		subtitlePath := req.Subtitle

		if !isPathAllowed(videoPath) || !isPathAllowed(subtitlePath) {
			log.Printf("Access denied for paths outside of allowed directories: %s, %s", videoPath, subtitlePath)
			http.Error(w, "Access denied: paths must be within MOVIES_PATH or SHOWS_PATH", http.StatusForbidden)
			return
		}

		// Acquire lock to serialize sync tasks
		syncMutex.Lock()
		defer syncMutex.Unlock()

		log.Printf("Starting subtitle sync for video: %s, subtitle: %s", filepath.Base(videoPath), filepath.Base(subtitlePath))

		// ffsubsync <video> -i <subtitle> -o <subtitle>
		// We use the same path for input and output, directly overwriting the file.
		// Wrapped in 'nice -n 15' to give it lower CPU priority.
		cmd := exec.Command("nice", "-n", "15", "ffsubsync", videoPath, "-i", subtitlePath, "-o", subtitlePath)

		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("ffsubsync failed: %v\nOutput: %s", err, string(output))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Failed to process subtitle",
				"details": string(output),
			})
			return
		}

		log.Printf("Successfully synced subtitle: %s", filepath.Base(subtitlePath))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Start server
	log.Printf("Starting server on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatal(err)
	}
}
