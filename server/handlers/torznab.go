package handlers

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Arrgo/services/indexers"
	"Arrgo/services"
	"github.com/justbri/arrgo/shared/config"
)

// TorznabError represents an error response in Torznab format
type TorznabError struct {
	XMLName xml.Name `xml:"error"`
	Code    string   `xml:"code,attr"`
	Desc    string   `xml:"description,attr"`
}

// TorznabCaps represents the capabilities response
type TorznabCaps struct {
	XMLName xml.Name `xml:"caps"`
	Server  struct {
		Version string `xml:"version,attr"`
		Title   string `xml:"title,attr"`
		URL     string `xml:"url,attr"`
	} `xml:"server"`
	Limits struct {
		Max     int `xml:"max,attr"`
		Default int `xml:"default,attr"`
	} `xml:"limits"`
	Searching struct {
		Search      SearchCapability `xml:"search"`
		TVSearch    SearchCapability `xml:"tv-search"`
		MovieSearch SearchCapability `xml:"movie-search"`
	} `xml:"searching"`
	Categories struct {
		Categories []Category `xml:"category"`
	} `xml:"categories"`
}

type SearchCapability struct {
	Available      string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

type Category struct {
	ID      string   `xml:"id,attr"`
	Name    string   `xml:"name,attr"`
	Subcats []Subcat `xml:"subcat,omitempty"`
}

type Subcat struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

// TorznabRSS represents the RSS feed response for search results
type TorznabRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title       string        `xml:"title"`
		Description string        `xml:"description"`
		Link        string        `xml:"link"`
		Language    string        `xml:"language"`
		Items       []TorznabItem `xml:"item"`
	} `xml:"channel"`
	TorznabNS string `xml:"xmlns:torznab,attr"`
}

type TorznabItem struct {
	Title       string           `xml:"title"`
	Guid        string           `xml:"guid"`
	Link        string           `xml:"link"`
	PubDate     string           `xml:"pubDate"`
	Description string           `xml:"description"`
	Enclosure   TorznabEnclosure `xml:"enclosure"`
	Attributes  []TorznabAttr    `xml:"torznab:attr"`
}

type TorznabEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type TorznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// TorznabAPIHandler handles all Torznab API requests
func TorznabAPIHandler(w http.ResponseWriter, r *http.Request) {
	// Get function parameter (required)
	function := strings.ToLower(r.URL.Query().Get("t"))
	if function == "" {
		writeTorznabError(w, "100", "Missing parameter: t (function)")
		return
	}

	// Get API key (optional for caps, but we'll check it for other functions)
	apiKey := r.URL.Query().Get("apikey")
	requiredAPIKey := config.GetEnv("TORZNAB_API_KEY", "")
	
	// Caps doesn't require auth, but other functions do
	if function != "caps" && requiredAPIKey != "" && apiKey != requiredAPIKey {
		writeTorznabError(w, "100", "Invalid API key")
		return
	}

	ctx := r.Context()

	switch function {
	case "caps":
		handleCaps(w, r)
	case "search", "tvsearch", "movie":
		handleSearch(w, r, ctx, function)
	default:
		writeTorznabError(w, "201", fmt.Sprintf("Unknown function: %s", function))
	}
}

// handleCaps returns the capabilities of the Torznab API
func handleCaps(w http.ResponseWriter, r *http.Request) {
	caps := TorznabCaps{}
	caps.Server.Version = "1.3"
	caps.Server.Title = "Arrgo Indexer"
	caps.Server.URL = getBaseURL(r)
	caps.Limits.Max = 100
	caps.Limits.Default = 50
	caps.Searching.Search.Available = "yes"
	caps.Searching.Search.SupportedParams = "q"
	caps.Searching.TVSearch.Available = "yes"
	caps.Searching.TVSearch.SupportedParams = "q,tvdbid,season,ep"
	caps.Searching.MovieSearch.Available = "yes"
	caps.Searching.MovieSearch.SupportedParams = "q,imdbid"

	// Standard Torznab categories
	caps.Categories.Categories = []Category{
		{ID: "2000", Name: "Movies"},
		{ID: "5000", Name: "TV"},
		{ID: "5030", Name: "TV:SD"},
		{ID: "5040", Name: "TV:HD"},
		{ID: "5045", Name: "TV:UHD"},
		{ID: "5050", Name: "TV:Other"},
		{ID: "5070", Name: "TV:Anime"},
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(caps); err != nil {
		slog.Error("Failed to encode caps response", "error", err)
	}
}

// handleSearch handles search, tvsearch, and movie search functions
func handleSearch(w http.ResponseWriter, r *http.Request, ctx context.Context, function string) {
	// Get query parameters
	query := r.URL.Query().Get("q")
	tvdbid := r.URL.Query().Get("tvdbid")
	imdbid := r.URL.Query().Get("imdbid")
	seasonStr := r.URL.Query().Get("season")
	episodeStr := r.URL.Query().Get("ep")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	cat := r.URL.Query().Get("cat")
	extended := r.URL.Query().Get("extended") == "1"

	// Parse limit and offset
	limit := 50 // default
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > 100 {
				limit = 100 // max
			}
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Parse season/episode
	season := 0
	if seasonStr != "" {
		if s, err := strconv.Atoi(seasonStr); err == nil {
			season = s
		}
	}

	episode := 0
	if episodeStr != "" {
		if e, err := strconv.Atoi(episodeStr); err == nil {
			episode = e
		}
	}

	// Determine search type and query
	searchType := "movie"
	searchQuery := query

	if function == "tvsearch" || function == "search" {
		if tvdbid != "" {
			// For TVDB ID searches, we'd need to look up the show name
			// For now, use the query if provided
			if query == "" {
				writeTorznabError(w, "201", "Query parameter 'q' required when tvdbid is provided")
				return
			}
		}
		if function == "tvsearch" || season > 0 {
			searchType = "show"
		}
	}

	if function == "movie" {
		if imdbid != "" && query == "" {
			// For IMDB ID searches, we'd need to look up the movie name
			// For now, require query
			writeTorznabError(w, "201", "Query parameter 'q' required")
			return
		}
	}

	if searchQuery == "" {
		writeTorznabError(w, "201", "Query parameter 'q' is required")
		return
	}

	// Perform search using Arrgo's search service
	seasonsParam := ""
	if season > 0 {
		seasonsParam = strconv.Itoa(season)
	}
	
	results, err := services.SearchTorrents(ctx, searchQuery, searchType, seasonsParam)
	if err != nil {
		slog.Warn("Search failed", "error", err)
		writeTorznabError(w, "500", fmt.Sprintf("Search failed: %v", err))
		return
	}

	// Filter by category if specified
	if cat != "" {
		results = filterByCategory(results, cat, searchType)
	}

	// Apply pagination
	totalResults := len(results)
	if offset >= totalResults {
		results = []indexers.SearchResult{}
	} else {
		end := offset + limit
		if end > totalResults {
			end = totalResults
		}
		results = results[offset:end]
	}

	// Convert to Torznab RSS format
	rss := convertToTorznabRSS(results, getBaseURL(r), extended)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	if err := encoder.Encode(rss); err != nil {
		slog.Error("Failed to encode Torznab RSS response", "error", err)
	}
}

// convertToTorznabRSS converts SearchResult to Torznab RSS format
func convertToTorznabRSS(results []indexers.SearchResult, baseURL string, extended bool) TorznabRSS {
	rss := TorznabRSS{
		Version:   "2.0",
		TorznabNS: "http://torznab.com/schemas/2015/feed",
	}
	rss.Channel.Title = "Arrgo Indexer"
	rss.Channel.Description = "Arrgo Torznab API"
	rss.Channel.Link = baseURL
	rss.Channel.Language = "en-us"

	items := make([]TorznabItem, 0, len(results))
	for _, result := range results {
		item := convertToTorznabItem(result, baseURL, extended)
		items = append(items, item)
	}
	rss.Channel.Items = items

	return rss
}

// convertToTorznabItem converts a SearchResult to a TorznabItem
func convertToTorznabItem(result indexers.SearchResult, baseURL string, extended bool) TorznabItem {
	item := TorznabItem{
		Title:       result.Title,
		Guid:        result.InfoHash,
		Link:        result.MagnetLink,
		PubDate:     time.Now().Format(time.RFC1123Z),
		Description: result.Title,
	}

	// Enclosure (magnet link)
	item.Enclosure.URL = result.MagnetLink
	item.Enclosure.Length = parseSizeToBytes(result.Size)
	item.Enclosure.Type = "application/x-bittorrent"

	// Extended attributes
	attrs := []TorznabAttr{
		{Name: "size", Value: parseSizeToBytes(result.Size)},
		{Name: "seeders", Value: strconv.Itoa(result.Seeds)},
		{Name: "peers", Value: strconv.Itoa(result.Peers)},
		{Name: "leechers", Value: strconv.Itoa(result.Peers - result.Seeds)},
	}

	if result.InfoHash != "" {
		attrs = append(attrs, TorznabAttr{Name: "infohash", Value: strings.ToLower(result.InfoHash)})
	}

	if result.MagnetLink != "" {
		attrs = append(attrs, TorznabAttr{Name: "magneturl", Value: result.MagnetLink})
	}

	// Category based on source and quality
	categoryID := determineCategory(result.Source, result.Resolution)
	attrs = append(attrs, TorznabAttr{Name: "category", Value: categoryID})

	// Extended attributes if requested
	if extended {
		if result.Resolution != "" {
			attrs = append(attrs, TorznabAttr{Name: "resolution", Value: result.Resolution})
		}
		if result.Quality != "" {
			attrs = append(attrs, TorznabAttr{Name: "video", Value: result.Quality})
		}
	}

	item.Attributes = attrs
	return item
}

// parseSizeToBytes converts size string (e.g., "1.5 GB") to bytes string
func parseSizeToBytes(sizeStr string) string {
	// If already a number, return as-is
	if size, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
		return strconv.FormatInt(size, 10)
	}

	// Parse human-readable format (e.g., "1.5 GB", "500 MB")
	sizeStr = strings.TrimSpace(sizeStr)
	sizeStr = strings.ToUpper(sizeStr)
	
	var size float64
	var unit string
	
	// Try to parse format like "1.5 GB" or "500MB"
	parts := strings.Fields(sizeStr)
	if len(parts) == 2 {
		fmt.Sscanf(parts[0], "%f", &size)
		unit = parts[1]
	} else if len(sizeStr) > 2 {
		// Try to parse "500MB" format
		var numStr string
		for i, r := range sizeStr {
			if r >= '0' && r <= '9' || r == '.' {
				numStr += string(r)
			} else {
				fmt.Sscanf(numStr, "%f", &size)
				unit = sizeStr[i:]
				break
			}
		}
	}

	multipliers := map[string]int64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}

	if mult, ok := multipliers[unit]; ok {
		bytes := int64(size * float64(mult))
		return strconv.FormatInt(bytes, 10)
	}

	// Fallback: return 0 if we can't parse
	return "0"
}

// determineCategory determines the Torznab category ID based on source and resolution
func determineCategory(source, resolution string) string {
	// Default categories
	// 2000 = Movies
	// 5000 = TV
	// 5040 = TV:HD
	// 5045 = TV:UHD
	
	resLower := strings.ToLower(resolution)
	
	if strings.Contains(resLower, "4k") || strings.Contains(resLower, "2160p") || strings.Contains(resLower, "uhd") {
		return "5045" // TV:UHD or could be movie, but defaulting to TV
	}
	if strings.Contains(resLower, "1080p") || strings.Contains(resLower, "1080i") {
		return "5040" // TV:HD
	}
	if strings.Contains(resLower, "720p") {
		return "5040" // TV:HD
	}
	
	// Default based on source
	if source == "YTS" {
		return "2000" // Movies
	}
	
	return "5000" // TV (default)
}

// filterByCategory filters results by Torznab category
func filterByCategory(results []indexers.SearchResult, catStr string, searchType string) []indexers.SearchResult {
	// Parse category IDs
	cats := strings.Split(catStr, ",")
	catMap := make(map[string]bool)
	for _, c := range cats {
		catMap[strings.TrimSpace(c)] = true
	}

	filtered := make([]indexers.SearchResult, 0)
	for _, result := range results {
		categoryID := determineCategory(result.Source, result.Resolution)
		if catMap[categoryID] {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// writeTorznabError writes an error response in Torznab format
func writeTorznabError(w http.ResponseWriter, code, description string) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusOK) // Torznab errors still return 200 OK
	
	encoder := xml.NewEncoder(w)
	encoder.Indent("", "  ")
	err := TorznabError{
		Code: code,
		Desc: description,
	}
	if encodeErr := encoder.Encode(err); encodeErr != nil {
		slog.Error("Failed to encode Torznab error", "error", encodeErr)
	}
}

// getBaseURL constructs the base URL from the request
func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
