package services

import (
	"Arrgo/config"
	"context"
	"strings"
)

// IsTorrentStillDownloading checks if a torrent is still downloading (not seeding)
// Returns true if downloading, false if seeding or not found
func IsTorrentStillDownloading(ctx context.Context, cfg *config.Config, torrentHash string) bool {
	if torrentHash == "" {
		return false // No torrent hash means it's not downloading
	}

	qb, err := NewQBittorrentClient(cfg)
	if err != nil {
		// If we can't connect to qBittorrent, assume it's not downloading
		// (safer to show it than hide it)
		return false
	}

	torrents, err := qb.GetTorrentsDetailed(ctx, "")
	if err != nil {
		return false
	}

	return IsTorrentStillDownloadingFromList(torrents, torrentHash)
}

// IsTorrentStillDownloadingFromList checks if a torrent is still downloading using a provided list of torrents
func IsTorrentStillDownloadingFromList(torrents []TorrentStatus, torrentHash string) bool {
	if torrentHash == "" {
		return false
	}

	normalizedHash := strings.ToLower(torrentHash)
	for _, torrent := range torrents {
		if strings.ToLower(torrent.Hash) == normalizedHash {
			// Check if torrent is in a downloading state
			state := strings.ToLower(torrent.State)
			downloadingStates := []string{
				"downloading",
				"metadl",     // downloading metadata
				"stalleddl",  // stalled downloading
				"queueddl",   // queued for download
				"checkingdl", // checking download
				"pauseddl",   // paused downloading
			}

			for _, dlState := range downloadingStates {
				if state == dlState {
					return true
				}
			}

			// If progress < 100% and not in a seeding state, it's likely still downloading
			if torrent.Progress < 1.0 {
				seedingStates := []string{
					"uploading",
					"stalledup",
					"queuedup",
					"pausedup",
				}
				isSeeding := false
				for _, seedState := range seedingStates {
					if state == seedState {
						isSeeding = true
						break
					}
				}
				if !isSeeding {
					return true
				}
			}

			// Otherwise, it's seeding or completed
			return false
		}
	}

	return false
}
