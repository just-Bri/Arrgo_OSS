package main

import (
	"Arrgo/config"
	"Arrgo/database"
	"Arrgo/handlers"
	"Arrgo/middleware"
	"Arrgo/services"
	"log"
	"net/http"
)

func main() {
	// Load configuration
	cfg := config.Load()

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

	// Setup routes
	mux := http.NewServeMux()

	// Static files
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Public routes
	mux.HandleFunc("/login", handlers.LoginHandler)
	mux.HandleFunc("/logout", handlers.LogoutHandler)

	// Protected routes
	mux.Handle("/dashboard", middleware.RequireAuth(http.HandlerFunc(handlers.DashboardHandler)))

	// Root redirect
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	})

	// Start server
	addr := ":" + cfg.ServerPort
	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal("Server failed:", err)
	}
}
