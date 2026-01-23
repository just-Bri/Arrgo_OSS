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

	if query == "" {
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

	// Log all errors, not just when results are empty
	if len(errs) > 0 {
		for _, err := range errs {
			slog.Error("Indexer search error", "error", err, "query", query, "type", searchType)
		}
	}

	if len(results) == 0 && len(errs) > 0 {
		if format == "json" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": errs[0].Error()})
		} else {
			http.Error(w, errs[0].Error(), http.StatusInternalServerError)
		}
		return
	}

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

	indexers := providers.GetIndexers()
	for _, idx := range indexers {
		var res []providers.SearchResult
		var err error

		switch {
		case searchType == "show" || searchType == "tv":
			// Skip indexers that only support movies
			if idx.GetName() == "YTS" {
				continue
			}

			// If multiple seasons requested, perform search for each
			if len(seasonNums) > 0 {
				for _, sn := range seasonNums {
					sRes, sErr := idx.SearchShows(ctx, query, sn, 0)
					if sErr == nil {
						res = append(res, sRes...)
					}
				}
			} else {
				res, err = idx.SearchShows(ctx, query, 0, 0)
			}
		default: // "movie", "solid" (general search) or empty
			// For movie searches, only use YTS indexer
			if idx.GetName() != "YTS" {
				continue
			}
			res, err = idx.SearchMovies(ctx, query)
		}

		if err != nil {
			errs = append(errs, err)
			continue
		}
		results = append(results, res...)
	}

	return results, errs
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
