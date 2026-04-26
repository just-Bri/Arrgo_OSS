package services

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func writeMovieNFO(dirPath, tmdbID string) {
	content := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<movie>
  <tmdbid>%s</tmdbid>
</movie>`, tmdbID)
	nfoPath := filepath.Join(dirPath, "movie.nfo")
	if err := os.WriteFile(nfoPath, []byte(content), 0644); err != nil {
		slog.Error("Failed to write movie NFO", "path", nfoPath, "error", err)
	}
}

func writeShowNFO(dirPath, tvdbID string) {
	content := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8" standalone="yes"?>
<tvshow>
  <tvdbid>%s</tvdbid>
</tvshow>`, tvdbID)
	nfoPath := filepath.Join(dirPath, "tvshow.nfo")
	if err := os.WriteFile(nfoPath, []byte(content), 0644); err != nil {
		slog.Error("Failed to write show NFO", "path", nfoPath, "error", err)
	}
}
