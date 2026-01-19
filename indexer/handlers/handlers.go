package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"text/template"

	"github.com/justbri/arrgo/indexer/providers"
)

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	tmpl, err := template.ParseFiles("templates/pages/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tmpl.Execute(w, nil)
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	searchType := r.URL.Query().Get("type") // "movie" or "tv"
	format := r.URL.Query().Get("format")   // "json" or "html" (default)

	if query == "" {
		return
	}

	var results []providers.SearchResult
	var errs []error
	ctx := r.Context()

	indexers := providers.GetIndexers()
	for _, idx := range indexers {
		if searchType == "tv" {
			// Skip movie-only indexers for TV searches
			if idx.GetName() == "YTS" {
				continue
			}
			res, errIdx := idx.SearchShows(ctx, query, 0, 0)
			if errIdx != nil {
				errs = append(errs, errIdx)
				continue
			}
			results = append(results, res...)
		} else if searchType == "movie" {
			// Skip 1337x for "movie" type if you want to prefer YTS
			if idx.GetName() == "1337x" {
				continue
			}
			res, errIdx := idx.SearchMovies(ctx, query)
			if errIdx != nil {
				errs = append(errs, errIdx)
				continue
			}
			results = append(results, res...)
		} else if searchType == "1337x" {
			// Specific 1337x search (shows everything)
			if idx.GetName() == "1337x" {
				res, errIdx := idx.SearchMovies(ctx, query)
				if errIdx != nil {
					errs = append(errs, errIdx)
					continue
				}
				results = append(results, res...)
			}
		}
	}

	if len(results) == 0 && len(errs) > 0 {
		log.Printf("Search error: %v", errs[0])
		http.Error(w, errs[0].Error(), http.StatusInternalServerError)
		return
	}

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
		return
	}

	// For now, let's just render a simple table of results
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
				{{range .}}
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
					<td colspan="4" class="p-8 text-center text-gray-500">No results found for "{{query}}"</td>
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

	tmpl.Execute(w, results)
}
