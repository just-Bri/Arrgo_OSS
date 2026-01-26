package indexers

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	sharedhttp "github.com/justbri/arrgo/shared/http"
)

// TorznabIndexer implements Indexer interface for external Torznab indexers
type TorznabIndexer struct {
	name   string
	baseURL string
	apiKey  string
}

// NewTorznabIndexer creates a new Torznab indexer
func NewTorznabIndexer(name, baseURL, apiKey string) *TorznabIndexer {
	return &TorznabIndexer{
		name:    name,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
	}
}

func (t *TorznabIndexer) GetName() string {
	return t.name
}

func (t *TorznabIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return t.search(ctx, "movie", query, 0, 0)
}

func (t *TorznabIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	return t.search(ctx, "tvsearch", query, season, episode)
}

func (t *TorznabIndexer) search(ctx context.Context, searchType, query string, season, episode int) ([]SearchResult, error) {
	apiURL := fmt.Sprintf("%s/api?t=%s&q=%s", t.baseURL, searchType, url.QueryEscape(query))
	
	if t.apiKey != "" {
		apiURL += "&apikey=" + url.QueryEscape(t.apiKey)
	}

	if season > 0 {
		apiURL += "&season=" + strconv.Itoa(season)
	}
	if episode > 0 {
		apiURL += "&ep=" + strconv.Itoa(episode)
	}

	slog.Info("Searching Torznab indexer", "name", t.name, "type", searchType, "query", query)

	resp, err := sharedhttp.MakeRequest(ctx, apiURL, sharedhttp.DefaultClient)
	if err != nil {
		return nil, fmt.Errorf("failed to query Torznab indexer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Torznab indexer returned status %d", resp.StatusCode)
	}

	// Parse XML response
	var rss TorznabRSS
	decoder := xml.NewDecoder(resp.Body)
	if err := decoder.Decode(&rss); err != nil {
		return nil, fmt.Errorf("failed to decode Torznab XML: %w", err)
	}

	results := make([]SearchResult, 0, len(rss.Channel.Items))
	for _, item := range rss.Channel.Items {
		result := convertTorznabItemToSearchResult(item, t.name)
		results = append(results, result)
	}

	return results, nil
}

type TorznabRSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []TorznabItem `xml:"item"`
	} `xml:"channel"`
}

type TorznabItem struct {
	Title       string           `xml:"title"`
	Guid        string           `xml:"guid"`
	Link        string           `xml:"link"`
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

func convertTorznabItemToSearchResult(item TorznabItem, source string) SearchResult {
	result := SearchResult{
		Title:      item.Title,
		MagnetLink: item.Enclosure.URL,
		Source:     source,
	}

	// Extract attributes
	for _, attr := range item.Attributes {
		switch attr.Name {
		case "seeders":
			if seeds, err := strconv.Atoi(attr.Value); err == nil {
				result.Seeds = seeds
			}
		case "peers":
			if peers, err := strconv.Atoi(attr.Value); err == nil {
				result.Peers = peers
			}
		case "infohash":
			result.InfoHash = attr.Value
		case "size":
			if size, err := strconv.ParseInt(attr.Value, 10, 64); err == nil {
				result.Size = formatBytes(size)
			}
		case "resolution":
			result.Resolution = attr.Value
		case "video":
			result.Quality = attr.Value
		}
	}

	// Use enclosure length if size not found
	if result.Size == "" && item.Enclosure.Length != "" {
		if size, err := strconv.ParseInt(item.Enclosure.Length, 10, 64); err == nil {
			result.Size = formatBytes(size)
		}
	}

	// Use link as magnet if enclosure URL is empty
	if result.MagnetLink == "" {
		result.MagnetLink = item.Link
	}

	return result
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
