package main

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/handlers"
	localmiddleware "Arrgo/middleware"
	"Arrgo/services"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sharedlogger "github.com/justbri/arrgo/shared/logger"
	sharedmiddleware "github.com/justbri/arrgo/shared/middleware"
	sharedserver "github.com/justbri/arrgo/shared/server"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// setupRoutes configures all HTTP routes
func setupRoutes() *chi.Mux {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.CleanPath)
	r.Use(sharedmiddleware.Logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Compress(5))

	// --- Static Files & Media ---
	fs := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))
	r.Get("/images/tmdb/*", handlers.ImageProxyHandler)
	r.Get("/images/movie/*", handlers.ServeMovieImage)
	r.Get("/images/shows/*", handlers.ServeShowImage)

	// --- Public Routes ---
	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	r.HandleFunc("/login", handlers.LoginHandler)
	r.HandleFunc("/register", handlers.RegisterHandler)
	r.HandleFunc("/logout", handlers.LogoutHandler)

	// --- Protected Routes ---
	r.Group(func(r chi.Router) {
		r.Use(localmiddleware.RequireAuth)

		// UI / General
		r.Get("/dashboard", handlers.DashboardHandler)
		r.Get("/search", handlers.SearchHandler)
		r.Get("/requests", handlers.RequestsHandler)
		r.Post("/requests/create", handlers.CreateRequestHandler)
		r.Post("/requests/delete", handlers.DeleteRequestHandler)

		// Movies
		r.Get("/movies", handlers.MoviesHandler)
		r.Get("/movies/details", handlers.MovieDetailsHandler)
		r.Post("/scan/movies", handlers.ScanMoviesHandler)
		r.Post("/scan/incoming/movies", handlers.ScanIncomingMoviesHandler)
		r.Post("/import/movies/all", handlers.ImportAllMoviesHandler)
		r.Post("/rename/movie", handlers.RenameMovieHandler)
		r.Post("/rename/library/movies", handlers.RenameAllLibraryMoviesHandler)
		r.Get("/api/movies/alternatives", handlers.GetMovieAlternativesHandler)
		r.Post("/api/movies/rematch", handlers.RematchMovieHandler)

		// Shows
		r.Get("/shows", handlers.ShowsHandler)
		r.Get("/shows/details", handlers.ShowDetailsHandler)
		r.Post("/scan/shows", handlers.ScanShowsHandler)
		r.Post("/scan/incoming/shows", handlers.ScanIncomingShowsHandler)
		r.Post("/import/shows/all", handlers.ImportAllShowsHandler)
		r.Post("/rename/show", handlers.RenameShowHandler)
		r.Post("/rename/library/shows", handlers.RenameAllLibraryShowsHandler)
		r.Get("/api/shows/alternatives", handlers.GetShowAlternativesHandler)
		r.Post("/api/shows/rematch", handlers.RematchShowHandler)

		// Subtitles
		r.Post("/subtitles/download", handlers.DownloadSubtitlesHandler)
		r.Post("/admin/subtitles/scan", handlers.ScanSubtitlesHandler)
		r.Post("/admin/subtitles/queue", handlers.QueueMissingSubtitlesHandler)
		r.Post("/api/admin/subtitles/sync/movie", handlers.MovieSubtitlesSyncHandler)
		r.Post("/api/admin/subtitles/sync/episode", handlers.EpisodeSubtitlesSyncHandler)
		r.Post("/api/admin/subtitles/sync/all", handlers.SyncAllSubtitlesHandler)

		// Admin & System
		r.Get("/admin", handlers.AdminHandler)
		r.Post("/admin/nuke", handlers.NukeLibraryHandler)
		r.Post("/scan/stop", handlers.StopScanHandler)
		r.Get("/api/scan/status", handlers.ScanStatusHandler)
		r.Post("/api/admin/dedupe/movies", handlers.DeduplicateMoviesHandler)
		r.Post("/api/admin/dedupe/shows", handlers.DeduplicateShowsHandler)
	})

	// Root redirect
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	return r
}

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize structured logging
	// logger.Init reads GOLOG_LOG_LEVEL directly from environment
	sharedlogger.Init(cfg.Environment, cfg.Debug)

	fmt.Println("-----------------------------------------")
	fmt.Println("Arrgo Process STDOUT Trigger")
	fmt.Println("-----------------------------------------")

	slog.Info("Initializing Arrgo components",
		"environment", cfg.Environment,
		"debug", cfg.Debug)

	// Initialize session store
	services.InitSessionStore(cfg)

	// Connect to database
	if err := database.Connect(cfg); err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// Initialize database schema
	if err := database.InitSchema(); err != nil {
		slog.Error("Failed to initialize database schema", "error", err)
		os.Exit(1)
	}

	// Seed admin user
	if err := database.SeedAdminUser(); err != nil {
		slog.Error("Failed to seed admin user", "error", err)
		os.Exit(1)
	}

	// Start background workers
	services.StartIncomingScanner(cfg)

	// Start completed requests cleanup worker (doesn't require qBittorrent)
	services.StartCompletedRequestsCleanupWorker()

	// Start Automation Service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	qb, err := services.NewQBittorrentClient(cfg)
	if err != nil {
		slog.Error("Failed to initialize qBittorrent client", "error", err)
		slog.Warn("Automation service will not start without qBittorrent client")
	} else {
		// Verify qBittorrent connectivity before starting automation
		testCtx, testCancel := context.WithTimeout(ctx, 10*time.Second)
		defer testCancel()
		if err := qb.Login(testCtx); err != nil {
			slog.Error("Failed to connect to qBittorrent, automation may not work", "error", err, "url", cfg.QBittorrentURL)
			slog.Warn("Automation service will start but may fail until qBittorrent is available")
		} else {
			slog.Info("Successfully connected to qBittorrent")
		}
		automation := services.NewAutomationService(cfg, qb)
		services.SetGlobalAutomationService(automation)
		go automation.Start(ctx)

		// Start seeding cleanup worker
		services.StartSeedingCleanupWorker(cfg, qb)
	}

	// Setup routes
	mux := setupRoutes()

	// Start server with graceful shutdown
	addr := ":" + cfg.ServerPort
	slog.Info("Arrgo starting",
		"address", addr,
		"environment", cfg.Environment,
		"debug", cfg.Debug)

	// Create HTTP server with shared configuration
	srvConfig := sharedserver.DefaultConfig(addr)
	srv := sharedserver.CreateServer(srvConfig, sharedmiddleware.Logging(mux))

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed to start", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("Server started successfully, waiting for shutdown signal")
	<-quit
	slog.Info("Shutting down server")

	// Cancel background context
	cancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Error during server shutdown", "error", err)
	} else {
		slog.Info("Server shutdown complete")
	}
}
