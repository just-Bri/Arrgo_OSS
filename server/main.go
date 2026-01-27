package main

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/handlers"
	"Arrgo/middleware"
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
)

// setupRoutes configures all HTTP routes
func setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Public routes
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	mux.HandleFunc("/login", handlers.LoginHandler)
	mux.HandleFunc("/register", handlers.RegisterHandler)
	mux.HandleFunc("/logout", handlers.LogoutHandler)
	mux.HandleFunc("/images/tmdb/", handlers.ImageProxyHandler)
	mux.HandleFunc("/images/movie/", handlers.ServeMovieImage)
	mux.HandleFunc("/images/shows/", handlers.ServeShowImage)

	// Protected routes - using helper to reduce repetition
	protectedRoutes := map[string]http.HandlerFunc{
		"/dashboard":               handlers.DashboardHandler,
		"/admin":                   handlers.AdminHandler,
		"/movies":                  handlers.MoviesHandler,
		"/movies/details":          handlers.MovieDetailsHandler,
		"/shows":                   handlers.ShowsHandler,
		"/shows/details":           handlers.ShowDetailsHandler,
		"/search":                  handlers.SearchHandler,
		"/requests":                handlers.RequestsHandler,
		"/requests/create":         handlers.CreateRequestHandler,
		"/requests/delete":         handlers.DeleteRequestHandler,
		"/scan/movies":             handlers.ScanMoviesHandler,
		"/scan/shows":              handlers.ScanShowsHandler,
		"/scan/incoming/movies":    handlers.ScanIncomingMoviesHandler,
		"/scan/incoming/shows":     handlers.ScanIncomingShowsHandler,
		"/scan/stop":               handlers.StopScanHandler,
		"/api/scan/status":         handlers.ScanStatusHandler,
		"/import/movies/all":       handlers.ImportAllMoviesHandler,
		"/import/shows/all":        handlers.ImportAllShowsHandler,
		"/rename/movie":            handlers.RenameMovieHandler,
		"/rename/show":             handlers.RenameShowHandler,
		"/subtitles/download":      handlers.DownloadSubtitlesHandler,
		"/admin/nuke":              handlers.NukeLibraryHandler,
		"/api/movies/alternatives": handlers.GetMovieAlternativesHandler,
		"/api/shows/alternatives":  handlers.GetShowAlternativesHandler,
		"/api/movies/rematch":      handlers.RematchMovieHandler,
		"/api/shows/rematch":       handlers.RematchShowHandler,
		"/indexers":                handlers.IndexersHandler,
		"/indexers/toggle":         handlers.ToggleIndexerHandler,
		"/indexers/add-builtin":    handlers.AddBuiltinIndexerHandler,
		"/indexers/delete":         handlers.DeleteIndexerHandler,
		"/indexers/reorder":        handlers.ReorderIndexersHandler,
	}

	for path, handler := range protectedRoutes {
		mux.Handle(path, middleware.RequireAuth(http.HandlerFunc(handler)))
	}

	// Root redirect
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		http.NotFound(w, r)
	})

	return mux
}

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize structured logging
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

	// Run migrations
	if err := database.RunMigrations(); err != nil {
		slog.Error("Failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Seed admin user
	if err := database.SeedAdminUser(); err != nil {
		slog.Error("Failed to seed admin user", "error", err)
		os.Exit(1)
	}

	// Seed default indexers
	if err := database.SeedDefaultIndexers(); err != nil {
		slog.Error("Failed to seed default indexers", "error", err)
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
