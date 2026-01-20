package services

import (
	"context"
	"sync"
)

type ScanType string

const (
	ScanIncomingMovies ScanType = "incoming_movies"
	ScanIncomingShows  ScanType = "incoming_shows"
	ScanMovieLibrary   ScanType = "movie_library"
	ScanShowLibrary    ScanType = "show_library"
)

var (
	scanCancels = make(map[ScanType]context.CancelFunc)
	scanMu      sync.RWMutex
)

func IsScanning(scanType ScanType) bool {
	scanMu.RLock()
	defer scanMu.RUnlock()
	_, active := scanCancels[scanType]
	return active
}

func StartScan(scanType ScanType) (context.Context, context.CancelFunc) {
	scanMu.Lock()
	defer scanMu.Unlock()

	if cancel, active := scanCancels[scanType]; active {
		return nil, cancel // Already scanning
	}

	ctx, cancel := context.WithCancel(context.Background())
	scanCancels[scanType] = cancel
	return ctx, cancel
}

func StopScan(scanType ScanType) {
	scanMu.Lock()
	defer scanMu.Unlock()

	if cancel, active := scanCancels[scanType]; active {
		cancel()
		delete(scanCancels, scanType)
	}
}

func FinishScan(scanType ScanType) {
	scanMu.Lock()
	defer scanMu.Unlock()
	delete(scanCancels, scanType)
}
