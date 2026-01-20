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
)

func init() {
	// Force logs to Stdout and remove timestamps for cleaner Docker logs
	log.SetOutput(os.Stdout)
	log.SetFlags(0)
}

func main() {
	// Load configuration
	cfg := config.Load()

	fmt.Println("-----------------------------------------")
	fmt.Println("Arrgo Process STDOUT Trigger")
	fmt.Println("-----------------------------------------")

	log.SetOutput(os.Stdout)
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
	qb, err := services.NewQBittorrentClient(cfg)
	if err != nil {
		log.Printf("Warning: Failed to initialize qBittorrent client: %v", err)
	} else {
		automation := services.NewAutomationService(cfg, qb)
		go automation.Start(context.Background())
	}

	// Setup routes
	mux := http.NewServeMux()

	// Static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Public routes
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/favicon.ico")
	})
	mux.HandleFunc("/site.webmanifest", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/site.webmanifest")
	})
	mux.HandleFunc("/login", handlers.LoginHandler)
	mux.HandleFunc("/register", handlers.RegisterHandler)
	mux.HandleFunc("/logout", handlers.LogoutHandler)
	mux.HandleFunc("/images/tmdb/", handlers.ImageProxyHandler)
	mux.HandleFunc("/images/movie/", handlers.ServeMovieImage)
	mux.HandleFunc("/images/shows/", handlers.ServeShowImage)

	// Protected routes
	mux.Handle("/dashboard", middleware.RequireAuth(http.HandlerFunc(handlers.DashboardHandler)))
	mux.Handle("/admin", middleware.RequireAuth(http.HandlerFunc(handlers.AdminHandler)))
	mux.Handle("/movies", middleware.RequireAuth(http.HandlerFunc(handlers.MoviesHandler)))
	mux.Handle("/movies/details", middleware.RequireAuth(http.HandlerFunc(handlers.MovieDetailsHandler)))
	mux.Handle("/shows", middleware.RequireAuth(http.HandlerFunc(handlers.ShowsHandler)))
	mux.Handle("/shows/details", middleware.RequireAuth(http.HandlerFunc(handlers.ShowDetailsHandler)))
	mux.Handle("/search", middleware.RequireAuth(http.HandlerFunc(handlers.SearchHandler)))
	mux.Handle("/requests", middleware.RequireAuth(http.HandlerFunc(handlers.RequestsHandler)))
	mux.Handle("/requests/create", middleware.RequireAuth(http.HandlerFunc(handlers.CreateRequestHandler)))
	mux.Handle("/requests/approve", middleware.RequireAuth(http.HandlerFunc(handlers.ApproveRequestHandler)))
	mux.Handle("/requests/deny", middleware.RequireAuth(http.HandlerFunc(handlers.DenyRequestHandler)))
	mux.Handle("/requests/delete", middleware.RequireAuth(http.HandlerFunc(handlers.DeleteRequestHandler)))
	mux.Handle("/scan/movies", middleware.RequireAuth(http.HandlerFunc(handlers.ScanMoviesHandler)))
	mux.Handle("/scan/shows", middleware.RequireAuth(http.HandlerFunc(handlers.ScanShowsHandler)))
	mux.Handle("/scan/incoming/movies", middleware.RequireAuth(http.HandlerFunc(handlers.ScanIncomingMoviesHandler)))
	mux.Handle("/scan/incoming/shows", middleware.RequireAuth(http.HandlerFunc(handlers.ScanIncomingShowsHandler)))
	mux.Handle("/scan/stop", middleware.RequireAuth(http.HandlerFunc(handlers.StopScanHandler)))
	mux.Handle("/import/movies/all", middleware.RequireAuth(http.HandlerFunc(handlers.ImportAllMoviesHandler)))
	mux.Handle("/import/shows/all", middleware.RequireAuth(http.HandlerFunc(handlers.ImportAllShowsHandler)))
	mux.Handle("/rename/movie", middleware.RequireAuth(http.HandlerFunc(handlers.RenameMovieHandler)))
	mux.Handle("/rename/show", middleware.RequireAuth(http.HandlerFunc(handlers.RenameShowHandler)))
	mux.Handle("/subtitles/download", middleware.RequireAuth(http.HandlerFunc(handlers.DownloadSubtitlesHandler)))
	mux.Handle("/admin/nuke", middleware.RequireAuth(http.HandlerFunc(handlers.NukeLibraryHandler)))

	// Root redirect
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			log.Printf("[ROUTE] Root path hit, redirecting to /dashboard")
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
			return
		}
		// Serve 404 for other paths not matched
		http.NotFound(w, r)
	})

	// Start server
	addr := ":" + cfg.ServerPort
	log.Printf("=========================================")
	log.Printf("Arrgo is starting on %s", addr)
	log.Printf("Environment: %s", cfg.Environment)
	log.Printf("Debug Mode: %v", cfg.Debug)
	log.Printf("=========================================")

	// Global logging middleware
	loggingMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[STDOUT-REQ] %s %s\n", r.Method, r.URL.Path)
		log.Printf("[LOG-REQ] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		mux.ServeHTTP(w, r)
	})

	if err := http.ListenAndServe(addr, loggingMux); err != nil {
		log.Fatalf("FATAL: Server failed to start: %v", err)
	}
}
