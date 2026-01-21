package providers

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/justbri/arrgo/shared/format"
)

type X1337Response struct {
	Status  string `json:"status"`
	Results []struct {
		Name     string `json:"name"`
		Size     string `json:"size"`
		Seeders  int    `json:"seeders"`
		Leechers int    `json:"leechers"`
		Magnet   string `json:"magnet"`
		Hash     string `json:"hash"`
	} `json:"results"`
}

type X1337Indexer struct{}

func NewX1337Indexer() *X1337Indexer {
	return &X1337Indexer{}
}

func (x *X1337Indexer) GetName() string {
	return "1337x"
}

func (x *X1337Indexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return x.search(ctx, query, "Movies")
}

func (x *X1337Indexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	return x.search(ctx, query, "TV")
}

func (x *X1337Indexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// 1337x doesn't have an official public API
	// Options:
	// 1. Use a proxy API service (if available)
	// 2. Use Jackett/Prowlarr which provides Torznab API for 1337x
	// 3. Implement HTML scraping (requires more maintenance)
	
	// Try using a proxy API endpoint if available
	// Note: These endpoints may not be stable - consider using Jackett/Prowlarr instead
	proxyURL := BuildQueryURL("https://1337x.wtf/api/search", map[string]string{
		"q": query,
	})

	resp, err := MakeHTTPRequest(ctx, proxyURL, DefaultHTTPClient)
	if err != nil {
		// Graceful degradation - return empty results instead of error
		// This allows other indexers to still work
		return []SearchResult{}, nil
	}

	var apiResp X1337Response
	if err := DecodeJSONResponse(resp, &apiResp); err != nil {
		// If JSON decode fails, return empty results (graceful degradation)
		return []SearchResult{}, nil
	}
	
	if apiResp.Status != "success" {
		return []SearchResult{}, nil
	}

	var results []SearchResult
	for _, r := range apiResp.Results {
		// Parse size string to bytes
		sizeBytes := parseSize(r.Size)
		
		// Extract quality/resolution from title
		quality, resolution := extractQualityInfo(r.Name)

		results = append(results, SearchResult{
			Title:      r.Name,
			Size:       format.Bytes(sizeBytes),
			Seeds:      r.Seeders,
			Peers:      r.Leechers,
			MagnetLink: r.Magnet,
			InfoHash:   r.Hash,
			Source:     "1337x",
			Resolution: resolution,
			Quality:    quality,
		})
	}

	return results, nil
}

// parseSize converts size string like "1.5 GB" to bytes
func parseSize(sizeStr string) int64 {
	if sizeStr == "" {
		return 0
	}

	// Remove commas and spaces
	sizeStr = strings.ReplaceAll(sizeStr, ",", "")
	sizeStr = strings.TrimSpace(sizeStr)

	// Match number and unit
	re := regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(KB|MB|GB|TB)`)
	matches := re.FindStringSubmatch(sizeStr)
	if len(matches) != 3 {
		return 0
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	unit := strings.ToUpper(matches[2])
	switch unit {
	case "KB":
		return int64(value * 1024)
	case "MB":
		return int64(value * 1024 * 1024)
	case "GB":
		return int64(value * 1024 * 1024 * 1024)
	case "TB":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	}

	return 0
}

// extractQualityInfo extracts quality and resolution from title
func extractQualityInfo(title string) (quality, resolution string) {
	titleLower := strings.ToLower(title)

	// Check for resolution
	if strings.Contains(titleLower, "2160p") || strings.Contains(titleLower, "4k") {
		resolution = "2160p"
		quality = "4K"
	} else if strings.Contains(titleLower, "1080p") {
		resolution = "1080p"
		quality = "1080p"
	} else if strings.Contains(titleLower, "720p") {
		resolution = "720p"
		quality = "720p"
	} else if strings.Contains(titleLower, "480p") {
		resolution = "480p"
		quality = "480p"
	}

	// Check for quality indicators
	if strings.Contains(titleLower, "bluray") || strings.Contains(titleLower, "bdrip") {
		quality = "BluRay"
	} else if strings.Contains(titleLower, "webrip") || strings.Contains(titleLower, "web-dl") {
		quality = "WebRip"
	} else if strings.Contains(titleLower, "dvdrip") {
		quality = "DVDRip"
	} else if strings.Contains(titleLower, "hdtv") {
		quality = "HDTV"
	}

	return quality, resolution
}
