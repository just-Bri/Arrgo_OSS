package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type OneThreeThreeSevenXIndexer struct {
	httpClient *http.Client
}

func New1337xIndexer() *OneThreeThreeSevenXIndexer {
	return &OneThreeThreeSevenXIndexer{
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (o *OneThreeThreeSevenXIndexer) GetName() string {
	return "1337x"
}

func (o *OneThreeThreeSevenXIndexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return o.search(ctx, query, "Movies")
}

func (o *OneThreeThreeSevenXIndexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	return o.search(ctx, query, "TV")
}

func (o *OneThreeThreeSevenXIndexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	// 1337x search URL format: https://1337x.to/category-search/query/category/1/
	searchURL := fmt.Sprintf("https://1337x.to/category-search/%s/%s/1/", url.PathEscape(query), category)
	if category == "" {
		searchURL = fmt.Sprintf("https://1337x.to/search/%s/1/", url.PathEscape(query))
	}

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	// Add user-agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch 1337x results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("1337x returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return o.parseSearchResults(ctx, string(body))
}

func (o *OneThreeThreeSevenXIndexer) parseSearchResults(ctx context.Context, html string) ([]SearchResult, error) {
	var results []SearchResult

	// Simple regex-based parsing for search results (quick and doesn't require extra libs)
	// This matches the rows in the search results table
	rowRegex := regexp.MustCompile(`(?s)<tr>.*?<td class="coll-1 name">.*?<a href="(/torrent/.*?)"\s*>(.*?)</a>.*?<td class="coll-2 seeds">(.*?)</td>.*?<td class="coll-3 leeches">(.*?)</td>.*?<td class="coll-4 size.*?">(.*?)<span`)
	matches := rowRegex.FindAllStringSubmatch(html, -1)

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		torrentPath := match[1]
		title := cleanHTML(match[2])
		seeds := stringToInt(match[3])
		peers := stringToInt(match[4])
		size := cleanHTML(match[5])

		// For 1337x, the magnet link is on the details page.
		// We'll resolve the first 3 results automatically to be helpful for automation
		magnet := "https://1337x.to" + torrentPath
		if len(results) < 3 {
			if resolved, err := o.resolveMagnet(ctx, magnet); err == nil {
				magnet = resolved
			}
		}

		results = append(results, SearchResult{
			Title:      title,
			Size:       size,
			Seeds:      seeds,
			Peers:      peers,
			MagnetLink: magnet,
			Source:     "1337x",
		})
	}

	return results, nil
}

func (o *OneThreeThreeSevenXIndexer) resolveMagnet(ctx context.Context, detailsURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", detailsURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// Extract magnet link
	magnetRegex := regexp.MustCompile(`href="(magnet:\?xt=urn:btih:[^"]+)"`)
	match := magnetRegex.FindStringSubmatch(html)
	if len(match) > 1 {
		return match[1], nil
	}

	return "", fmt.Errorf("magnet link not found on detail page")
}

func cleanHTML(s string) string {
	// Remove tags and extra whitespace
	r := regexp.MustCompile("<[^>]*>")
	return strings.TrimSpace(r.ReplaceAllString(s, ""))
}

func stringToInt(s string) int {
	var n int
	fmt.Sscanf(s, "%d", &n)
	return n
}
