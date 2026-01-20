package services

import (
	"Arrgo/config"
	"context"
	"log/slog"
	"time"
)

// StartIncomingScanner starts a background worker that scans the incoming folder every hour on the hour.
func StartIncomingScanner(cfg *config.Config) {
	slog.Info("Starting incoming media background scanner")

	go func() {
		for {
			now := time.Now()
			// Calculate time until next hour
			next := now.Truncate(time.Hour).Add(time.Hour)
			sleepDuration := time.Until(next)

			slog.Debug("Next incoming scan scheduled", "next_time", next.Format("15:04:05"), "sleep_duration", sleepDuration.Round(time.Second))

			// Wait until the next hour
			time.Sleep(sleepDuration)

			// Run the scan
			slog.Info("Running scheduled incoming media scan")
			
			// Scan movies
			if err := ScanMovies(context.Background(), cfg, true); err != nil {
				slog.Error("Error scanning movies", "error", err)
			}
			
			// Scan shows
			if err := ScanShows(context.Background(), cfg, true); err != nil {
				slog.Error("Error scanning shows", "error", err)
			}

			slog.Info("Scheduled incoming media scan complete")
		}
	}()
}
