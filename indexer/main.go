package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/justbri/arrgo/indexer/handlers"
	"github.com/justbri/arrgo/shared/config"
	"github.com/justbri/arrgo/shared/middleware"
	"github.com/justbri/arrgo/shared/server"
)

func main() {
	port := config.GetEnv("PORT", "5004")

	// Setup routes
	mux := setupRoutes()

	// Create server with shared configuration
	srvConfig := server.DefaultConfig(":" + port)
	srv := server.CreateServer(srvConfig, middleware.LoggingSimple(mux))

	fmt.Printf("Indexer service starting on port %s...\n", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
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

