package main

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/handlers"
	"Arrgo/middleware"
	"Arrgo/services"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sharedmiddleware "github.com/justbri/arrgo/shared/middleware"
	sharedserver "github.com/justbri/arrgo/shared/server"
)

func init() {
	// Force logs to Stdout and remove timestamps for cleaner Docker logs
	log.SetOutput(os.Stdout)
	log.SetFlags(0)
}

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
		"/dashboard":            handlers.DashboardHandler,
		"/admin":                handlers.AdminHandler,
		"/movies":               handlers.MoviesHandler,
		"/movies/details":       handlers.MovieDetailsHandler,
		"/shows":                handlers.ShowsHandler,
		"/shows/details":        handlers.ShowDetailsHandler,
		"/search":               handlers.SearchHandler,
		"/requests":             handlers.RequestsHandler,
		"/requests/create":      handlers.CreateRequestHandler,
		"/requests/approve":     handlers.ApproveRequestHandler,
		"/requests/deny":        handlers.DenyRequestHandler,
		"/requests/delete":      handlers.DeleteRequestHandler,
		"/scan/movies":          handlers.ScanMoviesHandler,
		"/scan/shows":           handlers.ScanShowsHandler,
		"/scan/incoming/movies": handlers.ScanIncomingMoviesHandler,
		"/scan/incoming/shows":  handlers.ScanIncomingShowsHandler,
		"/scan/stop":            handlers.StopScanHandler,
		"/import/movies/all":    handlers.ImportAllMoviesHandler,
		"/import/shows/all":     handlers.ImportAllShowsHandler,
		"/rename/movie":         handlers.RenameMovieHandler,
		"/rename/show":          handlers.RenameShowHandler,
		"/subtitles/download":   handlers.DownloadSubtitlesHandler,
		"/admin/nuke":           handlers.NukeLibraryHandler,
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

	fmt.Println("-----------------------------------------")
	fmt.Println("Arrgo Process STDOUT Trigger")
	fmt.Println("-----------------------------------------")

	// Configure logging for runtime (init() already set basic config)
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Printf("Initializing Arrgo components...")

	// Initialize session store
	services.InitSessionStore(cfg)

	// Connect to database
	if err := database.Connect(cfg); err != nil {
		log.Fatal("Failed to connect to database:", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.RunMigrations(); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	// Seed admin user
	if err := database.SeedAdminUser(); err != nil {
		log.Fatal("Failed to seed admin user:", err)
	}

	// Start background workers
	services.StartIncomingScanner(cfg)

	// Start Automation Service
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	qb, err := services.NewQBittorrentClient(cfg)
	if err != nil {
		log.Printf("Warning: Failed to initialize qBittorrent client: %v", err)
	} else {
		automation := services.NewAutomationService(cfg, qb)
		go automation.Start(ctx)
	}

	// Setup routes
	mux := setupRoutes()

	// Start server with graceful shutdown
	addr := ":" + cfg.ServerPort
	log.Printf("=========================================")
	log.Printf("Arrgo is starting on %s", addr)
	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Debug Mode: %v", cfg.Debug)
	log.Printf("=========================================")

	// Create HTTP server with shared configuration
	srvConfig := sharedserver.DefaultConfig(addr)
	srv := sharedserver.CreateServer(srvConfig, sharedmiddleware.Logging(mux))

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("FATAL: Server failed to start: %v", err)
		}
	}()

	log.Printf("Server started successfully. Waiting for shutdown signal...")
	<-quit
	log.Printf("Shutting down server...")

	// Cancel background context
	cancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	} else {
		log.Printf("Server shutdown complete")
	}
}
