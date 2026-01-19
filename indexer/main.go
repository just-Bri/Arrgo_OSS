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

	// Static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	fmt.Printf("Indexer service starting on port %s...\n", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
