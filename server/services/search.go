package services

import (
	"context"
	"Arrgo/services/indexers"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// SearchTorrents searches across all enabled indexers
func SearchTorrents(ctx context.Context, query, searchType string, seasons string) ([]indexers.SearchResult, error) {
	indexerList, err := indexers.GetIndexers()
	if err != nil {
		return nil, fmt.Errorf("failed to get indexers: %w", err)
	}

	var results []indexers.SearchResult
	var errs []error

	// Parse seasons for show searches
	var seasonNums []int
	if seasons != "" {
		seasonStrs := strings.Split(seasons, ",")
		for _, s := range seasonStrs {
			if num, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				seasonNums = append(seasonNums, num)
			}
		}
	}

	if searchType == "show" || searchType == "tv" {
		// For shows, use all indexers except YTS (which only supports movies)
		slog.Info("Searching for show", "query", query, "seasons", seasons)
		for _, idx := range indexerList {
			if idx.GetName() == "YTS" {
				slog.Debug("Skipping YTS indexer for show search")
				continue
			}

			var res []indexers.SearchResult
			var err error

			// If multiple seasons requested, perform search for each
			if len(seasonNums) > 0 {
				for _, sn := range seasonNums {
					slog.Debug("Searching for specific season", "indexer", idx.GetName(), "season", sn)
					sRes, sErr := idx.SearchShows(ctx, query, sn, 0)
					if sErr == nil {
						res = append(res, sRes...)
					} else {
						slog.Warn("Season search failed", "indexer", idx.GetName(), "season", sn, "error", sErr)
					}
				}
			} else {
				res, err = idx.SearchShows(ctx, query, 0, 0)
			}

			if err != nil {
				slog.Warn("Indexer search failed", "indexer", idx.GetName(), "error", err)
				errs = append(errs, err)
				continue
			}
			results = append(results, res...)
		}
	} else {
		// Movie search
		slog.Info("Searching for movie", "query", query)
		for _, idx := range indexerList {
			res, err := idx.SearchMovies(ctx, query)
			if err != nil {
				slog.Warn("Indexer search failed", "indexer", idx.GetName(), "error", err)
				errs = append(errs, err)
				continue
			}
			results = append(results, res...)
		}
	}

	// Log errors but don't fail
	if len(errs) > 0 {
		for _, err := range errs {
			slog.Warn("Indexer search error", "error", err)
		}
	}

	return results, nil
}
