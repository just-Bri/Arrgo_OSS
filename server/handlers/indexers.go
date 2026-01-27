package handlers

import (
	"Arrgo/models"
	"Arrgo/services"
	"Arrgo/services/indexers"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"strconv"
)

var indexersTmpl *template.Template

func init() {
	var err error
	funcMap := GetFuncMap()
	indexersTmpl, err = template.New("indexers").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/indexers.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse indexers template:", err)
	}
}

type IndexersPageData struct {
	Username    string
	IsAdmin     bool
	CurrentPage string
	SearchQuery string
	Indexers    []models.Indexer
	Stats       map[string]interface{}
	Catalog     []indexers.IndexerCatalogEntry
}

// IndexersHandler displays the indexer management page
func IndexersHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	indexersList, err := services.GetIndexers()
	if err != nil {
		slog.Error("Error getting indexers", "error", err)
		indexersList = []models.Indexer{}
	}

	stats, err := services.GetIndexerStats()
	if err != nil {
		slog.Error("Error getting indexer stats", "error", err)
		stats = map[string]interface{}{}
	}

	// Get available scraper types for the UI
	scraperTypes := indexers.GetAvailableScraperTypes()
	if stats == nil {
		stats = make(map[string]interface{})
	}
	stats["scraper_types"] = scraperTypes

	// Get indexer catalog
	catalog := indexers.GetIndexerCatalog()

	data := IndexersPageData{
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		CurrentPage: "/indexers",
		SearchQuery: "",
		Indexers:    indexersList,
		Stats:       stats,
		Catalog:     catalog,
	}

	if err := indexersTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error rendering indexers template", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ToggleIndexerHandler toggles an indexer's enabled status
func ToggleIndexerHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	enabledStr := r.FormValue("enabled")
	enabled := enabledStr == "true" || enabledStr == "1"

	if err := services.ToggleIndexer(id, enabled); err != nil {
		slog.Error("Error toggling indexer", "error", err, "id", id)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated indexer for HTMX
	indexer, err := services.GetIndexerByID(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return HTML fragment for HTMX
	w.Header().Set("Content-Type", "text/html")
	renderIndexerRow(w, indexer)
}

// AddBuiltinIndexerHandler adds a new built-in indexer
func AddBuiltinIndexerHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := r.FormValue("name")
	scraperType := r.FormValue("scraper_type")
	configJSON := r.FormValue("config")
	priorityStr := r.FormValue("priority")

	if name == "" || scraperType == "" {
		http.Error(w, "Name and scraper type are required", http.StatusBadRequest)
		return
	}

	priority := 10 // default priority
	if priorityStr != "" {
		if p, err := strconv.Atoi(priorityStr); err == nil {
			priority = p
		}
	}

	indexer, err := services.AddBuiltinIndexerWithConfig(name, scraperType, configJSON, priority)
	if err != nil {
		slog.Error("Error adding built-in indexer", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return HTML fragment for HTMX
	w.Header().Set("Content-Type", "text/html")
	renderIndexerRow(w, indexer)
}

// DeleteIndexerHandler deletes an indexer
func DeleteIndexerHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodDelete && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		idStr = r.FormValue("id")
	}

	if idStr == "" {
		http.Error(w, "Missing id parameter", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid id parameter", http.StatusBadRequest)
		return
	}

	if err := services.DeleteIndexer(id); err != nil {
		slog.Error("Error deleting indexer", "error", err, "id", id)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(""))
}

// ReorderIndexersHandler reorders indexers by priority
func ReorderIndexersHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil || !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var indexerIDs []int
	if err := json.NewDecoder(r.Body).Decode(&indexerIDs); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if err := services.ReorderIndexers(indexerIDs); err != nil {
		slog.Error("Error reordering indexers", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// renderIndexerRow renders a single indexer row for HTMX updates
func renderIndexerRow(w http.ResponseWriter, indexer *models.Indexer) {
	enabledText := "Disabled"
	enabledColor := "var(--muted-text)"
	if indexer.Enabled {
		enabledText = "Enabled"
		enabledColor = "var(--success-color)"
	}

	typeBadge := "Built-in"

	urlDisplay := indexer.URL
	if urlDisplay == "" {
		urlDisplay = "—"
	}

	deleteButton := ""
	// Only allow deletion of custom (non-default) built-in indexers
	defaultNames := map[string]bool{
		"YTS":           true,
		"Nyaa":          true,
		"1337x":         true,
		"TorrentGalaxy": true,
		"SolidTorrents": true,
	}
	if !defaultNames[indexer.Name] {
		deleteButton = fmt.Sprintf(`<button class="btn-danger" 
			hx-delete="/indexers/delete?id=%d"
			hx-target="#indexer-%d"
			hx-swap="outerHTML"
			hx-confirm="Are you sure you want to delete this indexer?"
			style="padding: 6px 12px; background: #dc3545; color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 13px;">
			Delete
		</button>`, indexer.ID, indexer.ID)
	} else {
		deleteButton = `<span style="color: var(--muted-text); font-size: 13px;">Default</span>`
	}

	fmt.Fprintf(w, `<tr id="indexer-%d" class="indexer-row" style="border-bottom: 1px solid var(--border-color);">
		<td style="padding: 12px; color: var(--text-color);">%s</td>
		<td style="padding: 12px;">
			<span class="badge" style="background: var(--badge-bg); color: var(--badge-text); padding: 4px 8px; border-radius: 4px; font-size: 12px;">%s</span>
		</td>
		<td style="padding: 12px; color: var(--text-color);">%d</td>
		<td style="padding: 12px;">
			<label class="toggle-switch" style="display: inline-flex; align-items: center; gap: 10px; cursor: pointer;">
				<input type="checkbox" %s
					hx-post="/indexers/toggle?id=%d&enabled=%t"
					hx-target="#indexer-%d"
					hx-swap="outerHTML"
					style="width: 40px; height: 20px; cursor: pointer;">
				<span style="color: %s; font-size: 14px;">%s</span>
			</label>
		</td>
		<td style="padding: 12px; color: var(--muted-text); font-size: 13px; max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">%s</td>
		<td style="padding: 12px;">%s</td>
	</tr>`,
		indexer.ID,
		indexer.Name,
		typeBadge,
		indexer.Priority,
		func() string {
			if indexer.Enabled {
				return "checked"
			}
			return ""
		}(),
		indexer.ID,
		!indexer.Enabled,
		indexer.ID,
		enabledColor,
		enabledText,
		urlDisplay,
		deleteButton,
	)
}
