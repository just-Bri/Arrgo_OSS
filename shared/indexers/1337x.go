package indexers

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/justbri/arrgo/shared/format"
	sharedhttp "github.com/justbri/arrgo/shared/http"
	"golang.org/x/net/html"
)

type X1337Indexer struct{}

func NewX1337Indexer() *X1337Indexer {
	return &X1337Indexer{}
}

func (x *X1337Indexer) Name() string {
	return "1337x"
}

func (x *X1337Indexer) SearchMovies(ctx context.Context, query string) ([]SearchResult, error) {
	return x.search(ctx, query, "Movies")
}

func (x *X1337Indexer) SearchShows(ctx context.Context, query string, season, episode int) ([]SearchResult, error) {
	searchQuery := query
	if season > 0 && episode > 0 {
		searchQuery = fmt.Sprintf("%s S%02dE%02d", query, season, episode)
	} else if season > 0 {
		searchQuery = fmt.Sprintf("%s S%02d", query, season)
	}
	return x.search(ctx, searchQuery, "TV")
}

func (x *X1337Indexer) search(ctx context.Context, query string, category string) ([]SearchResult, error) {
	searchPath := url.PathEscape(query)
	searchURL := fmt.Sprintf("https://1337x.to/search/%s/1/", searchPath)

	slog.Debug("Fetching from 1337x", "query", query, "category", category, "url", searchURL)
	htmlContent, err := sharedhttp.FetchViaBypass(ctx, searchURL)
	if err != nil {
		slog.Debug("1337x request failed", "query", query, "category", category, "error", err)
		return []SearchResult{}, nil
	}

	results := x.parseSearchResults(htmlContent)

	if len(results) == 0 && strings.Contains(query, " S") {
		baseQuery := strings.Split(query, " S")[0]
		slog.Debug("No 1337x results for specific season, trying broad search", "base_query", baseQuery)
		broadURL := fmt.Sprintf("https://1337x.to/search/%s/1/", url.PathEscape(baseQuery))
		htmlContent, err = sharedhttp.FetchViaBypass(ctx, broadURL)
		if err == nil {
			results = x.parseSearchResults(htmlContent)
		}
	}

	if len(results) == 0 {
		slog.Debug("1337x returned 0 results", "query", query, "html_preview", format.Preview(htmlContent, 200))
	} else {
		slog.Debug("1337x request successful", "query", query, "results", len(results))
	}
	return results, nil
}

func (x *X1337Indexer) parseSearchResults(htmlContent string) []SearchResult {
	var results []SearchResult

	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return results
	}

	var findTorrentRows func(*html.Node) []*html.Node
	findTorrentRows = func(n *html.Node) []*html.Node {
		var rows []*html.Node

		if n.Type == html.ElementNode && n.Data == "tr" {
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

	for _, row := range torrentRows {
		var title, link string
		var seeders, leechers int
		var size string

		cellIndex := 0
		for c := row.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "td" {
				text := strings.TrimSpace(x.getTextContent(c))

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

				if cellIndex > 0 && text != "" {
					if val, err := strconv.Atoi(text); err == nil {
						if seeders == 0 {
							seeders = val
						} else if leechers == 0 {
							leechers = val
						}
					}

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

		if title != "" && link != "" {
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

// parseSize converts a size string like "1.5 GB" to bytes.
// Used by 1337x and TorrentGalaxy providers.
func parseSize(sizeStr string) int64 {
	if sizeStr == "" {
		return 0
	}

	sizeStr = strings.ReplaceAll(sizeStr, ",", "")
	sizeStr = strings.TrimSpace(sizeStr)

	re := regexp.MustCompile(`(?i)(\d+\.?\d*)\s*(KB|MB|GB|TB)`)
	matches := re.FindStringSubmatch(sizeStr)
	if len(matches) != 3 {
		return 0
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	switch strings.ToUpper(matches[2]) {
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

// extractQualityInfo extracts quality and resolution from a torrent title.
// Used by 1337x, Nyaa, and TorrentGalaxy providers.
func extractQualityInfo(title string) (quality, resolution string) {
	titleLower := strings.ToLower(title)

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
