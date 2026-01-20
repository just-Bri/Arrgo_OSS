package services

import (
	"Arrgo/config"
	"context"
	"log"
	"time"
)

// StartIncomingScanner starts a background worker that scans the incoming folder every hour on the hour.
func StartIncomingScanner(cfg *config.Config) {
	log.Printf("[WORKER] Starting incoming media background scanner...")

	go func() {
		for {
			now := time.Now()
			// Calculate time until next hour
			next := now.Truncate(time.Hour).Add(time.Hour)
			sleepDuration := time.Until(next)

			log.Printf("[WORKER] Next incoming scan scheduled for %s (in %v)", next.Format("15:04:05"), sleepDuration.Round(time.Second))

			// Wait until the next hour
			time.Sleep(sleepDuration)

			// Run the scan
			log.Printf("[WORKER] Running scheduled incoming media scan...")
			
			// Scan movies
			if err := ScanMovies(context.Background(), cfg, true); err != nil {
				log.Printf("[WORKER] Error scanning movies: %v", err)
			}
			
			// Scan shows
			if err := ScanShows(context.Background(), cfg, true); err != nil {
				log.Printf("[WORKER] Error scanning shows: %v", err)
			}

			log.Printf("[WORKER] Scheduled incoming media scan complete.")
		}
	}()
}
