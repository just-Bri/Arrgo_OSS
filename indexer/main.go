package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/justbri/arrgo/indexer/handlers"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5004"
	}

	// Routes
	http.HandleFunc("/", handlers.IndexHandler)
	http.HandleFunc("/search", handlers.SearchHandler)

	// Logging middleware
	logger := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("%s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
			next.ServeHTTP(w, r)
		})
	}

	// Static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	fmt.Printf("Indexer service starting on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, logger(http.DefaultServeMux)); err != nil {
		log.Fatal(err)
	}
}
