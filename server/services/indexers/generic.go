package indexers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	sharedconfig "github.com/justbri/arrgo/shared/config"
	"github.com/justbri/arrgo/shared/format"
	"golang.org/x/net/html"
)

// GenericScraperConfig defines how to scrape a generic indexer
type GenericScraperConfig struct {
	BaseURL           string            `json:"base_url"`            // e.g., "https://thepiratebay.org"
	SearchURLPattern  string            `json:"search_url_pattern"` // e.g., "/search/{query}/0/99/0" or "/search.php?q={query}"
	UseCloudflareBypass bool            `json:"use_cloudflare_bypass"` // Whether to use Flaresolverr
	Selectors         SelectorConfig   `json:"selectors"`           // CSS/XPath selectors for parsing
	MagnetExtraction  MagnetConfig     `json:"magnet_extraction"`  // How to extract magnet links
}

type SelectorConfig struct {
	ResultContainer string `json:"result_container"` // CSS selector for each result item (e.g., "tr", ".torrent-row")
	Title           string `json:"title"`            // CSS selector or XPath for title
	Size            string `json:"size"`             // CSS selector for size
	Seeds           string `json:"seeds"`            // CSS selector for seeders
	Peers           string `json:"peers"`            // CSS selector for leechers
	Link            string `json:"link"`             // CSS selector for torrent/magnet link
}

type MagnetConfig struct {
	Type            string `json:"type"`             // "magnet", "torrent_page", "direct"
	LinkSelector    string `json:"link_selector"`    // CSS selector for link element
	ExtractFromPage bool   `json:"extract_from_page"` // Whether to fetch detail page for magnet
	MagnetPattern   string `json:"magnet_pattern"`  // Regex pattern to extract magnet from page
}

// GenericIndexer implements Indexer interface using configurable scraping
type GenericIndexer struct {
	name   string
	config GenericScraperConfig
	bypassURL string
}

// NewGenericIndexer creates a new generic indexer from configuration
func NewGenericIndexer(name string, configJSON string) (*GenericIndexer, error) {
	var config GenericScraperConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("failed to parse generic scraper config: %w", err)
	}

	// Validate required fields
	if config.BaseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if config.SearchURLPattern == "" {
		return nil, fmt.Errorf("search_url_pattern is required")
	}

	bypassURL := ""
	if config.UseCloudflareBypass {
		bypassURLEnv := sharedconfig.GetEnv("CLOUDFLARE_BYPASS_URL", "http://192.168.10.11:8191")
		bypassURL = strings.TrimSuffix(bypassURLEnv, "/")
	}

	return &GenericIndexer{
		name:      name,
		config:    config,
		bypassURL: bypassURL,
	}, nil
}

func (g *GenericIndexer) GetName() string {
	return g.name
}

func (g *GenericIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return g.search(ctx, query, "")
}

func (g *GenericIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// Enhance query with season info if provided
	searchQuery := query
	if season > 0 {
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
		if episode > 0 {
			searchQuery = fmt.Sprintf("%s E%02d", searchQuery, episode)
		}
	}
	return g.search(ctx, searchQuery, "")
}

func (g *GenericIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// Build search URL
	searchURL := g.buildSearchURL(query)
	
	slog.Info("Searching generic indexer", "name", g.name, "query", query, "url", searchURL)

	// Fetch HTML content
	var htmlContent string
	var err error
	
	if g.config.UseCloudflareBypass && g.bypassURL != "" {
		htmlContent, err = g.fetchViaBypass(ctx, searchURL)
	} else {
		htmlContent, err = g.fetchDirect(ctx, searchURL)
	}
	
	if err != nil {
		slog.Warn("Generic indexer request failed", "name", g.name, "query", query, "error", err)
		return []SearchResult{}, nil
	}

	// Parse HTML and extract results
	results := g.parseSearchResults(htmlContent)
	slog.Info("Generic indexer search successful", "name", g.name, "query", query, "results", len(results))
	return results, nil
}

func (g *GenericIndexer) buildSearchURL(query string) string {
	// Replace {query} placeholder in pattern
	searchURL := strings.ReplaceAll(g.config.SearchURLPattern, "{query}", url.QueryEscape(query))
	
	// If pattern doesn't start with http, prepend base URL
	if !strings.HasPrefix(searchURL, "http") {
		baseURL := strings.TrimSuffix(g.config.BaseURL, "/")
		searchURL = baseURL + searchURL
	}
	
	return searchURL
}

func (g *GenericIndexer) fetchDirect(ctx context.Context, targetURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", err
	}
	
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	
	return string(body), nil
}

func (g *GenericIndexer) fetchViaBypass(ctx context.Context, targetURL string) (string, error) {
	// Flaresolverr-compatible API
	requestBody := map[string]interface{}{
		"cmd":        "request.get",
		"url":        targetURL,
		"maxTimeout": 60000,
	}
	
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", g.bypassURL+"/v1", strings.NewReader(string(jsonBody)))
	if err != nil {
		return "", err
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	
	if solution, ok := result["solution"].(map[string]interface{}); ok {
		if html, ok := solution["response"].(string); ok {
			return html, nil
		}
	}
	
	return "", fmt.Errorf("unexpected bypass response format")
}

func (g *GenericIndexer) parseSearchResults(htmlContent string) []SearchResult {
	var results []SearchResult
	
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return results
	}
	
	// Find result containers using selector
	containers := g.findElementsBySelector(doc, g.config.Selectors.ResultContainer)
	
	for _, container := range containers {
		result := g.extractResultFromContainer(container)
		if result.Title != "" {
			results = append(results, result)
		}
	}
	
	return results
}

func (g *GenericIndexer) findElementsBySelector(doc *html.Node, selector string) []*html.Node {
	// Simple tag-based selector (e.g., "tr", "div")
	// For more complex selectors, we'd need a CSS selector library
	var elements []*html.Node
	
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == selector {
			elements = append(elements, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	
	return elements
}

func (g *GenericIndexer) extractResultFromContainer(container *html.Node) SearchResult {
	result := SearchResult{
		Source: g.name,
	}
	
	// Extract title
	if g.config.Selectors.Title != "" {
		result.Title = g.extractText(container, g.config.Selectors.Title)
	}
	
	// Extract size
	if g.config.Selectors.Size != "" {
		sizeStr := g.extractText(container, g.config.Selectors.Size)
		result.Size = format.Bytes(parseSize(sizeStr))
	}
	
	// Extract seeds
	if g.config.Selectors.Seeds != "" {
		seedsStr := g.extractText(container, g.config.Selectors.Seeds)
		if seeds, err := strconv.Atoi(strings.TrimSpace(seedsStr)); err == nil {
			result.Seeds = seeds
		}
	}
	
	// Extract peers
	if g.config.Selectors.Peers != "" {
		peersStr := g.extractText(container, g.config.Selectors.Peers)
		if peers, err := strconv.Atoi(strings.TrimSpace(peersStr)); err == nil {
			result.Peers = peers
		}
	}
	
	// Extract link/magnet
	if g.config.Selectors.Link != "" {
		link := g.extractLink(container, g.config.Selectors.Link)
		if strings.HasPrefix(link, "magnet:") {
			result.MagnetLink = link
			// Extract info hash from magnet link if it's a magnet URL
			if strings.HasPrefix(link, "magnet:") {
				re := regexp.MustCompile(`btih:([a-fA-F0-9]{40})`)
				matches := re.FindStringSubmatch(link)
				if len(matches) > 1 {
					result.InfoHash = strings.ToLower(matches[1])
				}
			}
		} else if link != "" {
			// Build full URL if relative
			if !strings.HasPrefix(link, "http") {
				link = strings.TrimSuffix(g.config.BaseURL, "/") + link
			}
			result.MagnetLink = link
		}
	}
	
	// Extract quality/resolution from title
	if result.Title != "" {
		result.Quality, result.Resolution = extractQualityInfo(result.Title)
	}
	
	return result
}

func (g *GenericIndexer) extractText(node *html.Node, selector string) string {
	// Simple tag-based extraction
	// Find element with matching tag
	var findElement func(*html.Node, string) *html.Node
	findElement = func(n *html.Node, tag string) *html.Node {
		if n.Type == html.ElementNode && n.Data == tag {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := findElement(c, tag); found != nil {
				return found
			}
		}
		return nil
	}
	
	element := findElement(node, selector)
	if element == nil {
		return ""
	}
	
	return getTextContent(element)
}

func (g *GenericIndexer) extractLink(node *html.Node, selector string) string {
	// Find anchor tag
	var findLink func(*html.Node) string
	findLink = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					return attr.Val
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if link := findLink(c); link != "" {
				return link
			}
		}
		return ""
	}
	
	return findLink(node)
}

func getTextContent(n *html.Node) string {
	var text strings.Builder
	var extractText func(*html.Node)
	extractText = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			extractText(c)
		}
	}
	extractText(n)
	return strings.TrimSpace(text.String())
}

