package services

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	sharedindexers "github.com/justbri/arrgo/shared/indexers"
)

// SearchTorrents searches across all enabled indexers
func SearchTorrents(ctx context.Context, query, searchType string, seasons string, episodes string) ([]sharedindexers.SearchResult, error) {
	indexerList := sharedindexers.Indexers()

	var results []sharedindexers.SearchResult
	var errs []error

	// Parse seasons and episodes for show searches
	var seasonNums []int
	if seasons != "" {
		seasonStrs := strings.Split(seasons, ",")
		for _, s := range seasonStrs {
			if num, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				seasonNums = append(seasonNums, num)
			}
		}
	}

	var episodeID string
	if episodes != "" {
		// Just take the first one if multiple are provided (unlikely from current UI)
		episodeID = strings.TrimSpace(strings.Split(episodes, ",")[0])
	}

	if searchType == "show" || searchType == "tv" {
		// For shows, use all indexers except YTS (which only supports movies)
		slog.Debug("Searching for show", "query", query, "seasons", seasons, "episodes", episodes)
		for _, idx := range indexerList {
			if idx.Name() == "YTS" {
				slog.Debug("Skipping YTS indexer for show search")
				continue
			}

			var res []sharedindexers.SearchResult
			var err error

			// If specific episode requested
			if episodeID != "" {
				slog.Debug("Searching for specific episode", "indexer", idx.Name(), "episode", episodeID)
				// episodeID is "S01E01" - extract season and episode numbers if possible?
				// Most indexers can just take the string in the query, but SearchShows takes ints.
				// Let's see if we can parse "S01E01"
				s, e := 0, 0
				fmt.Sscanf(strings.ToLower(episodeID), "s%de%d", &s, &e)
				res, err = idx.SearchShows(ctx, query, s, e)
			} else if len(seasonNums) > 0 {
				// If multiple seasons requested, perform search for each
				for _, sn := range seasonNums {
					slog.Debug("Searching for specific season", "indexer", idx.Name(), "season", sn)
					sRes, sErr := idx.SearchShows(ctx, query, sn, 0)
					if sErr == nil {
						res = append(res, sRes...)
					} else {
						slog.Debug("Season search failed", "indexer", idx.Name(), "season", sn, "error", sErr)
					}
				}
			} else {
				res, err = idx.SearchShows(ctx, query, 0, 0)
			}

			if err != nil {
				slog.Debug("Indexer search failed", "indexer", idx.Name(), "error", err)
				errs = append(errs, err)
				continue
			}
			results = append(results, res...)
		}
	} else {
		// Movie search — YTS only. It's reliable and covers the vast majority of films.
		slog.Debug("Searching for movie", "query", query)
		for _, idx := range indexerList {
			if idx.Name() != "YTS" {
				continue
			}
			res, err := idx.SearchMovies(ctx, query)
			if err != nil {
				slog.Debug("Indexer search failed", "indexer", idx.Name(), "error", err)
				errs = append(errs, err)
				continue
			}
			results = append(results, res...)
		}
	}

	// Log errors at Debug level to avoid flooding but still allow troubleshooting
	if len(errs) > 0 {
		for _, err := range errs {
			slog.Debug("Indexer search error", "error", err)
		}
	}

	return results, nil
}
