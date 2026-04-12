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

type nyaaCacheEntry struct {
	results   []SearchResult
	timestamp time.Time
}

var nyaaCache = struct {
	sync.RWMutex
	entries map[string]*nyaaCacheEntry
	ttl     time.Duration
}{
	entries: make(map[string]*nyaaCacheEntry),
	ttl:     24 * time.Hour,
}

func NewNyaaIndexer() *NyaaIndexer {
	return &NyaaIndexer{}
}

func (n *NyaaIndexer) Name() string {
	return "Nyaa"
}

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
	return n.searchRSS(ctx, query, "0_0")
}

func (n *NyaaIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	searchQuery := query
	if season > 0 && episode > 0 {
		searchQuery = fmt.Sprintf("%s S%02dE%02d", query, season, episode)
	} else if season > 0 {
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	} else if episode > 0 {
		searchQuery = fmt.Sprintf("%s E%02d", query, episode)
	}
	return n.searchRSS(ctx, searchQuery, "1_2")
}

func (n *NyaaIndexer) searchRSS(ctx context.Context, query string, category string) ([]SearchResult, error) {
	cacheKey := fmt.Sprintf("%s:%s", query, category)

	nyaaCache.RLock()
	if entry, exists := nyaaCache.entries[cacheKey]; exists {
		if time.Since(entry.timestamp) < nyaaCache.ttl {
			nyaaCache.RUnlock()
			slog.Debug("Nyaa cache hit", "query", query, "category", category, "results", len(entry.results))
			return entry.results, nil
		}
		slog.Debug("Nyaa cache expired", "query", query, "category", category, "age", time.Since(entry.timestamp))
	}
	nyaaCache.RUnlock()

	slog.Debug("Fetching from Nyaa RSS", "query", query, "category", category)
	searchURL := sharedhttp.BuildQueryURL("https://nyaa.si/", map[string]string{
		"page": "rss",
		"q":    query,
		"c":    category,
	})

	resp, err := sharedhttp.MakeRequest(ctx, searchURL, sharedhttp.DefaultClient)
	if err != nil {
		slog.Debug("Nyaa RSS request failed", "query", query, "category", category, "error", err)
		return []SearchResult{}, nil
	}
	defer resp.Body.Close()

	var rss NyaaRSS
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		slog.Debug("Nyaa RSS decode failed", "query", query, "category", category, "error", err)
		return []SearchResult{}, nil
	}

	slog.Debug("Nyaa RSS request successful", "query", query, "category", category, "items", len(rss.Channel.Items))

	var results []SearchResult
	for _, item := range rss.Channel.Items {
		infoHash := item.Torrent.InfoHash
		magnetLink := item.Torrent.Magnet

		if magnetLink == "" {
			if infoHash != "" {
				magnetLink = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(item.Title))
			} else {
				continue
			}
		}

		if infoHash == "" && magnetLink != "" {
			infoHash = extractInfoHashFromMagnet(magnetLink)
		}

		sizeBytes := parseNyaaSize(item.Description)
		seeds, peers := parseNyaaStats(item.Description)
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

	nyaaCache.Lock()
	nyaaCache.entries[cacheKey] = &nyaaCacheEntry{
		results:   results,
		timestamp: time.Now(),
	}
	nyaaCache.Unlock()

	return results, nil
}

// CleanupNyaaCache removes expired cache entries. Call periodically to prevent memory leaks.
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

func parseNyaaSize(description string) int64 {
	re := regexp.MustCompile(`(?i)Size:\s*(\d+\.?\d*)\s*(KiB|MiB|GiB|TiB)`)
	matches := re.FindStringSubmatch(description)
	if len(matches) != 3 {
		return 0
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	switch strings.ToUpper(matches[2]) {
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

func parseNyaaStats(description string) (seeds int, peers int) {
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

func extractInfoHashFromMagnet(magnetLink string) string {
	prefix := "xt=urn:btih:"
	idx := strings.Index(magnetLink, prefix)
	if idx == -1 {
		return ""
	}

	start := idx + len(prefix)
	if start+40 > len(magnetLink) {
		return ""
	}

	hash := magnetLink[start : start+40]
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return ""
		}
	}

	return hash
}
