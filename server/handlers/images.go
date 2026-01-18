package handlers

import (
	"Arrgo/config"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func ImageProxyHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/images/tmdb/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	cfg := config.Load()
	// TMDB images don't require API key in the URL, but good to have for other things
	tmdbURL := fmt.Sprintf("https://image.tmdb.org/t/p/w500/%s", path)

	resp, err := http.Get(tmdbURL)
	if err != nil {
		http.Error(w, "Failed to fetch image", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "Image not found", resp.StatusCode)
		return
	}

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
