package indexers

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/justbri/arrgo/shared/format"
	sharedhttp "github.com/justbri/arrgo/shared/http"
)

type NyaaIndexer struct{}

// Cache entry for RSS feed results
type nyaaCacheEntry struct {
	results   []SearchResult
	timestamp time.Time
}

// In-memory cache for Nyaa RSS feeds
// Cache TTL: 24 hours (RSS feeds don't change frequently)
var nyaaCache = struct {
	sync.RWMutex
	entries map[string]*nyaaCacheEntry
	ttl     time.Duration
}{
	entries: make(map[string]*nyaaCacheEntry),
	ttl:     24 * time.Hour, // Cache for 24 hours
}

func NewNyaaIndexer() *NyaaIndexer {
	return &NyaaIndexer{}
}

func (n *NyaaIndexer) GetName() string {
	return "Nyaa"
}

// Nyaa RSS Feed Structure
type NyaaRSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			Torrent     struct {
				InfoHash string `xml:"infoHash"`
				Magnet   string `xml:"magnetURI"`
			} `xml:"torrent"`
		} `xml:"item"`
	} `xml:"channel"`
}

func (n *NyaaIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	// Nyaa is primarily for anime, but we can search it for movies too
	// Use category 1_0 (Anime) or 0_0 (All) - let's use All for movies
	return n.searchRSS(ctx, query, "0_0")
}

func (n *NyaaIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// Enhance query with season info if provided
	searchQuery := query
	if season > 0 {
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	// Use category 1_0 for Anime (English translated)
	return n.searchRSS(ctx, searchQuery, "1_2")
}

func (n *NyaaIndexer) searchRSS(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// Create cache key from query and category
	cacheKey := fmt.Sprintf("%s:%s", query, category)
	
	// Check cache first
	nyaaCache.RLock()
	if entry, exists := nyaaCache.entries[cacheKey]; exists {
		// Check if cache entry is still valid
		if time.Since(entry.timestamp) < nyaaCache.ttl {
			nyaaCache.RUnlock()
			// Return cached results
			slog.Debug("Nyaa cache hit", "query", query, "category", category, "results", len(entry.results))
			return entry.results, nil
		}
		slog.Debug("Nyaa cache expired", "query", query, "category", category, "age", time.Since(entry.timestamp))
	}
	nyaaCache.RUnlock()
	
	// Cache miss or expired - fetch from RSS feed
	slog.Info("Fetching from Nyaa RSS", "query", query, "category", category)
	// Nyaa.si RSS feed format: https://nyaa.si/?page=rss&q={query}&c={category}
	// Categories:
	//   0_0 = All categories
	//   1_0 = Anime
	//   1_2 = Anime - English translated
	//   1_3 = Anime - Non-English translated
	//   1_4 = Anime - Raw
	
	searchURL := sharedhttp.BuildQueryURL("https://nyaa.si/", map[string]string{
		"page": "rss",
		"q":    query,
		"c":    category,
	})

	resp, err := sharedhttp.MakeRequest(ctx, searchURL, sharedhttp.DefaultClient)
	if err != nil {
		slog.Warn("Nyaa RSS request failed", "query", query, "category", category, "error", err)
		// Graceful degradation - return empty results instead of error
		return []SearchResult{}, nil
	}
	defer resp.Body.Close()

	var rss NyaaRSS
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		slog.Warn("Nyaa RSS decode failed", "query", query, "category", category, "error", err)
		// If XML decode fails, return empty results (graceful degradation)
		return []SearchResult{}, nil
	}
	
	slog.Info("Nyaa RSS request successful", "query", query, "category", category, "items", len(rss.Channel.Items))

	var results []SearchResult
	for _, item := range rss.Channel.Items {
		// Extract info hash from magnet link if not in torrent tag
		infoHash := item.Torrent.InfoHash
		magnetLink := item.Torrent.Magnet
		
		// If no magnet in torrent tag, try to extract from link or construct from infohash
		if magnetLink == "" {
			if infoHash != "" {
				// Construct magnet link from info hash
				magnetLink = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(item.Title))
			} else {
				// Try to extract from link (Nyaa pages have magnet links)
				// For now, skip if we can't get a magnet link
				continue
			}
		}
		
		// Extract info hash from magnet if we have it
		if infoHash == "" && magnetLink != "" {
			infoHash = extractInfoHashFromMagnet(magnetLink)
		}

		// Parse size from description (format: "Size: 1.2 GiB")
		sizeBytes := parseNyaaSize(item.Description)
		
		// Extract seeders/leechers from description (format: "Seeders: 123, Leechers: 45")
		seeds, peers := parseNyaaStats(item.Description)

		// Extract quality and resolution from title
		quality, resolution := extractQualityInfo(item.Title)

		results = append(results, SearchResult{
			Title:      item.Title,
			Size:       format.Bytes(sizeBytes),
			Seeds:      seeds,
			Peers:      peers,
			MagnetLink: magnetLink,
			InfoHash:   infoHash,
			Source:     "Nyaa",
			Resolution: resolution,
			Quality:    quality,
		})
	}

	// Store results in cache
	nyaaCache.Lock()
	nyaaCache.entries[cacheKey] = &nyaaCacheEntry{
		results:   results,
		timestamp: time.Now(),
	}
	nyaaCache.Unlock()

	return results, nil
}

// CleanupNyaaCache removes expired entries from the cache
// This can be called periodically to prevent memory leaks
func CleanupNyaaCache() {
	nyaaCache.Lock()
	defer nyaaCache.Unlock()
	
	now := time.Now()
	for key, entry := range nyaaCache.entries {
		if now.Sub(entry.timestamp) >= nyaaCache.ttl {
			delete(nyaaCache.entries, key)
		}
	}
}

// parseNyaaSize extracts size from Nyaa RSS description
// Format: "Size: 1.2 GiB" or "Size: 500 MiB"
func parseNyaaSize(description string) int64 {
	// Match pattern like "Size: 1.2 GiB" or "Size: 500 MiB"
	re := regexp.MustCompile(`(?i)Size:\s*(\d+\.?\d*)\s*(KiB|MiB|GiB|TiB)`)
	matches := re.FindStringSubmatch(description)
	if len(matches) != 3 {
		return 0
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	unit := strings.ToUpper(matches[2])
	switch unit {
	case "KIB":
		return int64(value * 1024)
	case "MIB":
		return int64(value * 1024 * 1024)
	case "GIB":
		return int64(value * 1024 * 1024 * 1024)
	case "TIB":
		return int64(value * 1024 * 1024 * 1024 * 1024)
	}

	return 0
}

// parseNyaaStats extracts seeders and leechers from Nyaa RSS description
// Format: "Seeders: 123, Leechers: 45"
func parseNyaaStats(description string) (seeds int, peers int) {
	// Match pattern like "Seeders: 123" and "Leechers: 45"
	seederRe := regexp.MustCompile(`(?i)Seeders:\s*(\d+)`)
	leecherRe := regexp.MustCompile(`(?i)Leechers:\s*(\d+)`)

	if matches := seederRe.FindStringSubmatch(description); len(matches) == 2 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			seeds = val
		}
	}

	if matches := leecherRe.FindStringSubmatch(description); len(matches) == 2 {
		if val, err := strconv.Atoi(matches[1]); err == nil {
			peers = val
		}
	}

	return seeds, peers
}

// extractInfoHashFromMagnet extracts the info hash from a magnet link
func extractInfoHashFromMagnet(magnetLink string) string {
	// Magnet link format: magnet:?xt=urn:btih:HASH&dn=...
	prefix := "xt=urn:btih:"
	idx := strings.Index(magnetLink, prefix)
	if idx == -1 {
		return ""
	}

	start := idx + len(prefix)
	// Info hash is 40 hex characters
	if start+40 > len(magnetLink) {
		return ""
	}

	hash := magnetLink[start : start+40]
	// Validate it's hex
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return ""
		}
	}

	return hash
}
