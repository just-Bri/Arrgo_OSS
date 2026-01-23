package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/justbri/arrgo/shared/config"
	"github.com/justbri/arrgo/shared/format"
	sharedhttp "github.com/justbri/arrgo/shared/http"
	"golang.org/x/net/html"
)

type X1337Indexer struct {
	bypassURL string
}

func NewX1337Indexer() *X1337Indexer {
	// Get Cloudflare bypass service URL from environment
	bypassURL := config.GetEnv("CLOUDFLARE_BYPASS_URL", "http://192.168.10.11:8191")
	// Ensure no trailing slash
	bypassURL = strings.TrimSuffix(bypassURL, "/")
	return &X1337Indexer{
		bypassURL: bypassURL,
	}
}

func (x *X1337Indexer) GetName() string {
	return "1337x"
}

func (x *X1337Indexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return x.search(ctx, query, "Movies")
}

func (x *X1337Indexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	// Enhance query with season info if provided
	searchQuery := query
	if season > 0 {
		// Try multiple formats: "Show Name S02" and "Show Name Season 2"
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	return x.search(ctx, searchQuery, "TV")
}

func (x *X1337Indexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// Build 1337x search URL
	// 1337x search format: https://1337x.to/search/{query}/1/
	searchPath := url.PathEscape(query)
	searchURL := fmt.Sprintf("https://1337x.to/search/%s/1/", searchPath)

	// Use Cloudflare bypass service to fetch the page
	htmlContent, err := x.fetchViaBypass(ctx, searchURL)
	if err != nil {
		// Graceful degradation - return empty results instead of error
		return []SearchResult{}, nil
	}

	// Parse HTML and extract torrent results
	results := x.parseSearchResults(htmlContent)
	return results, nil
}

// fetchViaBypass uses the Cloudflare bypass service (Flaresolverr-compatible) to fetch a URL
func (x *X1337Indexer) fetchViaBypass(ctx context.Context, targetURL string) (string, error) {
	// Flaresolverr-compatible API format
	// POST to /v1 with JSON body
	requestBody := map[string]interface{}{
		"cmd":       "request.get",
		"url":       targetURL,
		"maxTimeout": 60000,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make POST request to bypass service
	req, err := http.NewRequestWithContext(ctx, "POST", x.bypassURL+"/v1", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a longer timeout client for Flaresolverr requests (can take up to 60s + buffer)
	// Flaresolverr maxTimeout is 60000ms, so we need at least 90s to account for network overhead
	client := &http.Client{
		Timeout: 90 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call bypass service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("bypass service returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse Flaresolverr response
	var bypassResp struct {
		Status string `json:"status"`
		Solution struct {
			URL      string `json:"url"`
			Response string `json:"response"`
			Cookies  []interface{} `json:"cookies"`
		} `json:"solution"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&bypassResp); err != nil {
		return "", fmt.Errorf("failed to decode bypass response: %w", err)
	}

	if bypassResp.Status != "ok" || bypassResp.Solution.Response == "" {
		return "", fmt.Errorf("bypass service returned invalid response")
	}

	return bypassResp.Solution.Response, nil
}

// parseSearchResults parses HTML from 1337x search results page
func (x *X1337Indexer) parseSearchResults(htmlContent string) []SearchResult {
	var results []SearchResult

	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return results
	}

	// Find all table rows that contain torrent links
	var findTorrentRows func(*html.Node) []*html.Node
	findTorrentRows = func(n *html.Node) []*html.Node {
		var rows []*html.Node
		
		if n.Type == html.ElementNode && n.Data == "tr" {
			// Check if this row contains a link to /torrent/
			hasTorrentLink := false
			var walk func(*html.Node)
			walk = func(node *html.Node) {
				if node.Type == html.ElementNode && node.Data == "a" {
					for _, attr := range node.Attr {
						if attr.Key == "href" && strings.Contains(attr.Val, "/torrent/") {
							hasTorrentLink = true
							return
						}
					}
				}
				for c := node.FirstChild; c != nil; c = c.NextSibling {
					walk(c)
				}
			}
			walk(n)
			
			if hasTorrentLink {
				rows = append(rows, n)
			}
		}
		
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			rows = append(rows, findTorrentRows(c)...)
		}
		
		return rows
	}

	torrentRows := findTorrentRows(doc)

	// Extract data from each row
	for _, row := range torrentRows {
		var title, link string
		var seeders, leechers int
		var size string
		
		cellIndex := 0
		for c := row.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "td" {
				text := strings.TrimSpace(x.getTextContent(c))
				
				// Try to find link in this cell
				var findLink func(*html.Node)
				findLink = func(node *html.Node) {
					if node.Type == html.ElementNode && node.Data == "a" {
						for _, attr := range node.Attr {
							if attr.Key == "href" && strings.Contains(attr.Val, "/torrent/") {
								link = attr.Val
								title = strings.TrimSpace(x.getTextContent(node))
								return
							}
						}
					}
					for child := node.FirstChild; child != nil; child = child.NextSibling {
						findLink(child)
					}
				}
				findLink(c)
				
				// Parse numeric values (seeders, leechers)
				if cellIndex > 0 && text != "" {
					// Try to parse as number (could be seeders or leechers)
					if val, err := strconv.Atoi(text); err == nil {
						if seeders == 0 {
							seeders = val
						} else if leechers == 0 {
							leechers = val
						}
					}
					
					// Check if this looks like a size (contains MB, GB, etc.)
					if strings.Contains(strings.ToUpper(text), "MB") || 
					   strings.Contains(strings.ToUpper(text), "GB") ||
					   strings.Contains(strings.ToUpper(text), "KB") ||
					   strings.Contains(strings.ToUpper(text), "TB") {
						size = text
					}
				}
				
				cellIndex++
			}
		}
		
		// Create result if we have title and link
		if title != "" && link != "" {
			// Build full URL if needed
			magnetLink := link
			if !strings.HasPrefix(link, "http") {
				magnetLink = "https://1337x.to" + link
			}
			
			sizeBytes := parseSize(size)
			quality, resolution := extractQualityInfo(title)
			
			results = append(results, SearchResult{
				Title:      title,
				Size:       format.Bytes(sizeBytes),
				Seeds:      seeders,
				Peers:      leechers,
				MagnetLink: magnetLink,
				InfoHash:   "",
				Source:     "1337x",
				Resolution: resolution,
				Quality:    quality,
			})
		}
	}

	return results
}

// getTextContent extracts all text content from a node
func (x *X1337Indexer) getTextContent(n *html.Node) string {
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
	return text.String()
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
