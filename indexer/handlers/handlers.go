package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
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
	if err := indexTmpl.Execute(w, nil); err != nil {
		log.Printf("Error rendering index template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type") // "movie", "show", or "solid"
	format := r.URL.Query().Get("format")   // "json" or "html" (default)

	if query == "" {
		return
	}

	results, errs := performSearch(r.Context(), query, searchType)

	if len(results) == 0 && len(errs) > 0 {
		log.Printf("Search error: %v", errs[0])
		http.Error(w, errs[0].Error(), http.StatusInternalServerError)
		return
	}

	if format == "json" {
		writeJSONResponse(w, results)
		return
	}

	writeHTMLResponse(w, results, query)
}

// performSearch executes the search across all indexers based on search type
func performSearch(ctx context.Context, query, searchType string) ([]providers.SearchResult, []error) {
	var results []providers.SearchResult
	var errs []error

	indexers := providers.GetIndexers()
	for _, idx := range indexers {
		var res []providers.SearchResult
		var err error

		switch {
		case searchType == "show" || searchType == "tv":
			// Skip movie-only indexers for TV searches
			if idx.GetName() == "YTS" {
				continue
			}
			res, err = idx.SearchShows(ctx, query, 0, 0)
		case searchType == "solid":
			// Specific Solid search (shows everything)
			if idx.GetName() == "SolidTorrents" {
				res, err = idx.SearchMovies(ctx, query)
			} else {
				continue
			}
		default: // "movie" or empty
			// Skip Solid for "movie" type if you want to prefer YTS
			if idx.GetName() == "SolidTorrents" {
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
		log.Printf("Error encoding JSON response: %v", err)
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
					<th class="p-3 text-center">Size</th>
					<th class="p-3 text-center">Seeds</th>
					<th class="p-3 text-center">Action</th>
				</tr>
			</thead>
			<tbody>
				{{range .Results}}
				<tr class="border-t border-gray-700 hover:bg-gray-750">
					<td class="p-3">
						<div class="font-bold text-blue-300">{{.Title}}</div>
						<div class="text-xs text-gray-400">{{.Source}} • {{.Resolution}}</div>
					</td>
					<td class="p-3 text-center text-sm">{{.Size}}</td>
					<td class="p-3 text-center text-green-400 font-mono">{{.Seeds}}</td>
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
		log.Printf("Error executing results template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
