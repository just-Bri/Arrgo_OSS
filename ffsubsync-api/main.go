package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	sharedlogger "github.com/justbri/arrgo/shared/logger"
	sharedmiddleware "github.com/justbri/arrgo/shared/middleware"
)

type SyncRequest struct {
	Video    string `json:"video"`
	Subtitle string `json:"subtitle"`
}

var (
	// syncMutex ensures only one ffsubsync process runs at a time to prevent CPU exhaustion.
	syncMutex sync.Mutex

	allowedPaths []string
)

func isPathAllowed(path string) bool {
	// If no paths are configured, allow all (backward compatibility/default behavior)
	if len(allowedPaths) == 0 {
		return true
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for _, p := range allowedPaths {
		if p != "" && strings.HasPrefix(absPath, p) {
			return true
		}
	}

	return false
}

func main() {
	allowedPaths = []string{
		os.Getenv("MOVIES_PATH"),
		os.Getenv("SHOWS_PATH"),
		os.Getenv("INCOMING_MOVIES_PATH"),
		os.Getenv("INCOMING_SHOWS_PATH"),
	}
	env := os.Getenv("ENV")
	if env == "" {
		env = "development"
	}
	debug := os.Getenv("DEBUG") == "true"

	// Initialize structured logging
	sharedlogger.Init(env, debug)

	slog.Info("SubSync API starting...", "allowed_paths", allowedPaths)

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.CleanPath)
	r.Use(sharedmiddleware.Logging)
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
			slog.Warn("Access denied for paths outside of allowed directories",
				"video_path", videoPath,
				"subtitle_path", subtitlePath)
			http.Error(w, "Access denied: paths must be within MOVIES_PATH or SHOWS_PATH", http.StatusForbidden)
			return
		}

		// Acquire lock to serialize sync tasks
		syncMutex.Lock()
		defer syncMutex.Unlock()

		slog.Info("Starting subtitle sync",
			"video", filepath.Base(videoPath),
			"subtitle", filepath.Base(subtitlePath))

		// ffsubsync <video> -i <subtitle> -o <subtitle>
		// We use the same path for input and output, directly overwriting the file.
		// Wrapped in 'nice -n 15' to give it lower CPU priority.
		cmd := exec.Command("nice", "-n", "15", "ffsubsync", videoPath, "-i", subtitlePath, "-o", subtitlePath)

		output, err := cmd.CombinedOutput()
		if err != nil {
			slog.Error("ffsubsync failed",
				"error", err,
				"output", string(output))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":   "Failed to process subtitle",
				"details": string(output),
			})
			return
		}

		slog.Info("Successfully synced subtitle",
			"subtitle", filepath.Base(subtitlePath))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "success"})
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Starting server on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("Shutdown signal received, waiting for in-flight syncs to complete...")

	// Allow up to 30 minutes for any in-flight ffsubsync process to finish before exiting.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server shutdown error", "error", err)
	} else {
		slog.Info("Server shutdown complete")
	}
}
