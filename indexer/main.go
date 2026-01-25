package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/justbri/arrgo/indexer/handlers"
	"github.com/justbri/arrgo/indexer/providers"
	"github.com/justbri/arrgo/shared/config"
	sharedlogger "github.com/justbri/arrgo/shared/logger"
	"github.com/justbri/arrgo/shared/middleware"
	"github.com/justbri/arrgo/shared/server"
)

func main() {
	port := config.GetEnv("PORT", "5004")
	env := config.GetEnv("ENV", "development")
	debug := config.GetEnv("DEBUG", "false") == "true"

	// Initialize structured logging
	sharedlogger.Init(env, debug)

	// Setup routes
	mux := setupRoutes()

	// Start periodic cache cleanup for Nyaa RSS cache
	go startCacheCleanup()

	// Create server with shared configuration
	srvConfig := server.DefaultConfig(":" + port)
	srv := server.CreateServer(srvConfig, middleware.LoggingSimple(mux))

	slog.Info("Indexer service starting", "port", port)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("Server failed to start", "error", err)
		os.Exit(1)
	}
}

// setupRoutes configures all HTTP routes
func setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Routes
	mux.HandleFunc("/", handlers.IndexHandler)
	mux.HandleFunc("/search", handlers.SearchHandler)

	// Static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	return mux
}

// startCacheCleanup runs periodic cleanup of expired cache entries
// Cleans up every 6 hours to prevent memory leaks
func startCacheCleanup() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		providers.CleanupNyaaCache()
		slog.Debug("Cleaned up expired Nyaa RSS cache entries")
	}
}

