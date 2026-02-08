package handlers

import (
	"context"
	"encoding/json"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"text/template"

	"github.com/justbri/arrgo/indexer/providers"
)

var indexTmpl *template.Template

func init() {
	var err error
	indexTmpl, err = template.ParseFiles("templates/pages/index.html")
	if err != nil {
		log.Fatal("Failed to parse index template:", err)
	}
}

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	indexers := providers.GetIndexers()
	var names []string
	for _, idx := range indexers {
		names = append(names, idx.GetName())
	}

	data := struct {
		Sources []string
	}{
		Sources: names,
	}

	if err := indexTmpl.Execute(w, data); err != nil {
		slog.Error("Error rendering index template", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type") // "movie", "show", or "solid"
	seasons := r.URL.Query().Get("seasons") // Comma-separated season numbers for shows
	format := r.URL.Query().Get("format")   // "json" or "html" (default)

	// Log search request parameters
	slog.Info("Search request received",
		"query", query,
		"type", searchType,
		"seasons", seasons,
		"format", format,
		"remote_addr", r.RemoteAddr)

	if query == "" {
		slog.Warn("Search request missing query parameter")
		if format == "json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "query parameter 'q' is required"})
		} else {
			http.Error(w, "query parameter 'q' is required", http.StatusBadRequest)
		}
		return
	}

	results, errs := performSearch(r.Context(), query, searchType, seasons)
	
	// Log search results summary
	slog.Info("Search completed",
		"query", query,
		"type", searchType,
		"results_count", len(results),
		"errors_count", len(errs))

	// Log all errors, but don't fail the request - return empty results instead
	// This allows the caller to handle "no results" gracefully rather than treating it as a fatal error
	if len(errs) > 0 {
		for _, err := range errs {
			slog.Warn("Indexer search error (returning empty results)", "error", err, "query", query, "type", searchType)
		}
	}

	// Return empty results with 200 OK status instead of 500 error
	// This is more appropriate for a search API - "no results" is not a server error
	if format == "json" {
		writeJSONResponse(w, results)
		return
	}

	writeHTMLResponse(w, results, query)
}

// performSearch executes the search across all indexers based on search type
func performSearch(ctx context.Context, query, searchType string, seasons string) ([]providers.SearchResult, []error) {
	var results []providers.SearchResult
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

	switch {
	case searchType == "show" || searchType == "tv":
		// For shows, use all indexers except YTS (which only supports movies)
		slog.Info("Searching for show", "query", query, "seasons", seasons)
		indexers := providers.GetIndexers()
		for _, idx := range indexers {
			if idx.GetName() == "YTS" {
				slog.Debug("Skipping YTS indexer for show search")
				continue
			}

			slog.Info("Searching indexer for show", "indexer", idx.GetName(), "query", query, "seasons", seasons)
			var res []providers.SearchResult
			var err error

			// If multiple seasons requested, perform search for each
			if len(seasonNums) > 0 {
				for _, sn := range seasonNums {
					slog.Debug("Searching for specific season", "indexer", idx.GetName(), "season", sn)
					sRes, sErr := idx.SearchShows(ctx, query, sn, 0)
					if sErr == nil {
						res = append(res, sRes...)
						slog.Debug("Season search completed", "indexer", idx.GetName(), "season", sn, "results", len(sRes))
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
			slog.Info("Indexer search completed", "indexer", idx.GetName(), "results", len(res))
			results = append(results, res...)
		}

	default: // "movie", "solid" (general search) or empty
		// For movie searches, use priority order:
		// 1. YTS (highest quality, known good source)
		// 2. Nyaa/1337x (fallback for older movies and anime)
		// 3. SolidTorrents/TorrentGalaxy (last resort)
		
		slog.Info("Searching for movie", "query", query)
		allIndexers := providers.GetIndexers()
		
		// Priority groups for movies
		priority1 := []string{"YTS"}                              // Highest priority
		priority2 := []string{"Nyaa", "1337x"}                    // Fallback
		priority3 := []string{"SolidTorrents", "TorrentGalaxy"}    // Last resort
		
		// Search in priority order
		searchedIndexers := make(map[string]bool)
		
		// Priority 1: YTS
		slog.Info("Searching priority 1 indexers", "query", query)
		for _, idx := range allIndexers {
			if contains(priority1, idx.GetName()) {
				slog.Info("Searching indexer", "indexer", idx.GetName(), "priority", 1, "query", query)
				res, err := idx.SearchMovies(ctx, query)
				searchedIndexers[idx.GetName()] = true
				if err != nil {
					slog.Warn("Indexer search failed", "indexer", idx.GetName(), "priority", 1, "error", err)
					errs = append(errs, err)
				} else {
					slog.Info("Indexer search completed", "indexer", idx.GetName(), "priority", 1, "results", len(res))
					results = append(results, res...)
				}
			}
		}
		
		// Priority 2: Nyaa, 1337x, TorrentGalaxy (only if YTS had no results or errors)
		if len(results) == 0 {
			slog.Info("No results from priority 1, searching priority 2 indexers", "query", query)
			for _, idx := range allIndexers {
				if !searchedIndexers[idx.GetName()] && contains(priority2, idx.GetName()) {
					slog.Info("Searching indexer", "indexer", idx.GetName(), "priority", 2, "query", query)
					res, err := idx.SearchMovies(ctx, query)
					searchedIndexers[idx.GetName()] = true
					if err != nil {
						slog.Warn("Indexer search failed", "indexer", idx.GetName(), "priority", 2, "error", err)
						errs = append(errs, err)
					} else {
						slog.Info("Indexer search completed", "indexer", idx.GetName(), "priority", 2, "results", len(res))
						results = append(results, res...)
					}
				}
			}
		} else {
			slog.Info("Skipping priority 2 indexers - found results in priority 1", "results_count", len(results))
		}
		
		// Priority 3: SolidTorrents (only if no results from higher priority sources)
		if len(results) == 0 {
			slog.Info("No results from priority 2, searching priority 3 indexers", "query", query)
			for _, idx := range allIndexers {
				if !searchedIndexers[idx.GetName()] && contains(priority3, idx.GetName()) {
					slog.Info("Searching indexer", "indexer", idx.GetName(), "priority", 3, "query", query)
					res, err := idx.SearchMovies(ctx, query)
					if err != nil {
						slog.Warn("Indexer search failed", "indexer", idx.GetName(), "priority", 3, "error", err)
						errs = append(errs, err)
					} else {
						slog.Info("Indexer search completed", "indexer", idx.GetName(), "priority", 3, "results", len(res))
						results = append(results, res...)
					}
				}
			}
		} else {
			slog.Info("Skipping priority 3 indexers - found results in higher priorities", "results_count", len(results))
		}
	}

	return results, errs
}

// contains checks if a string slice contains a specific string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// writeJSONResponse writes search results as JSON
func writeJSONResponse(w http.ResponseWriter, results []providers.SearchResult) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		slog.Error("Error encoding JSON response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// writeHTMLResponse writes search results as HTML table
func writeHTMLResponse(w http.ResponseWriter, results []providers.SearchResult, query string) {
	type TemplateData struct {
		Results []providers.SearchResult
		Query   string
	}

	tmplContent := `
	<div class="overflow-x-auto">
		<table class="w-full text-left bg-gray-800 rounded-lg overflow-hidden">
			<thead class="bg-gray-700">
				<tr>
					<th class="p-3">Title</th>
					<th class="p-3 text-center">Source</th>
					<th class="p-3 text-center">Size</th>
					<th class="p-3 text-center">Seeds</th>
					<th class="p-3 text-center">Action</th>
				</tr>
			</thead>
			<tbody>
				{{range .Results}}
				<tr class="border-t border-gray-700 hover:bg-gray-750 transition-colors">
					<td class="p-3">
						<div class="font-bold text-blue-300 leading-tight">{{.Title}}</div>
						<div class="text-xs text-gray-400 mt-1">
							{{if .Quality}}<span class="bg-gray-700 px-1 rounded text-gray-300">{{.Quality}}</span>{{end}}
							{{if .Resolution}}<span class="bg-blue-900/30 px-1 rounded text-blue-300 ml-1">{{.Resolution}}</span>{{end}}
						</div>
					</td>
					<td class="p-3 text-center">
						<span class="text-xs font-semibold px-2 py-0.5 rounded bg-gray-700 text-gray-300 border border-gray-600">
							{{.Source}}
						</span>
					</td>
					<td class="p-3 text-center text-sm font-mono">{{.Size}}</td>
					<td class="p-3 text-center text-green-400 font-mono font-bold">{{.Seeds}}</td>
					<td class="p-3 text-center">
						<a href="{{.MagnetLink}}" class="bg-green-600 hover:bg-green-700 text-white px-3 py-1 rounded text-sm transition">
							Magnet
						</a>
					</td>
				</tr>
				{{else}}
				<tr>
					<td colspan="4" class="p-8 text-center text-gray-500">No results found for "{{.Query}}"</td>
				</tr>
				{{end}}
			</tbody>
		</table>
	</div>`

	tmpl, err := template.New("results").Parse(tmplContent)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := TemplateData{
		Results: results,
		Query:   query,
	}

	if err := tmpl.Execute(w, data); err != nil {
		slog.Error("Error executing results template", "error", err, "query", query)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
