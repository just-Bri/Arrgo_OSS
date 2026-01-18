package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
)

var requestsTmpl *template.Template

func init() {
	var err error
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}
	requestsTmpl, err = template.New("requests").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/requests.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse requests template:", err)
	}
}

type RequestsData struct {
	Username string
	Requests []models.Request
}

func RequestsHandler(w http.ResponseWriter, r *http.Request) {
	session, err := services.GetSession(r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	userID := session.Values["user_id"]
	if userID == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	user, err := services.GetUserByID(interfaceToInt64(userID))
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	requests, err := services.GetRequests()
	if err != nil {
		log.Printf("Error getting requests: %v", err)
		requests = []models.Request{}
	}

	data := RequestsData{
		Username: user.Username,
		Requests: requests,
	}

	if err := requestsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func SearchHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]interface{}{})
		return
	}

	cfg := config.Load()
	
	// Search both TMDB and TVDB
	movieResults, err := services.SearchTMDB(cfg, query)
	if err != nil {
		log.Printf("TMDB search error: %v", err)
	}

	showResults, err := services.SearchTVDB(cfg, query)
	if err != nil {
		log.Printf("TVDB search error: %v", err)
	}

	// Combine results
	type EnhancedResult struct {
		services.SearchResult
		LibraryStatus services.LibraryStatus `json:"library_status"`
	}

	var combined []EnhancedResult
	
	for _, res := range movieResults {
		status, _ := services.CheckLibraryStatus("movie", res.ID)
		combined = append(combined, EnhancedResult{
			SearchResult:  res,
			LibraryStatus: status,
		})
	}

	for _, res := range showResults {
		status, _ := services.CheckLibraryStatus("show", res.ID)
		combined = append(combined, EnhancedResult{
			SearchResult:  res,
			LibraryStatus: status,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(combined)
}

func CreateRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	session, err := services.GetSession(r)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	userID := session.Values["user_id"]
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req models.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.UserID = int(interfaceToInt64(userID))

	// Final server-side check to prevent duplicate requests or requesting library items
	var externalID string
	if req.MediaType == "movie" {
		externalID = req.TMDBID
	} else {
		externalID = req.TVDBID
	}

	status, err := services.CheckLibraryStatus(req.MediaType, externalID)
	if err != nil {
		log.Printf("Error checking library status: %v", err)
		http.Error(w, "Failed to verify media status", http.StatusInternalServerError)
		return
	}

	if status.Exists || strings.Contains(status.Message, "Already requested") {
		http.Error(w, "Media already exists or has been requested", http.StatusConflict)
		return
	}

	if err := services.CreateRequest(req); err != nil {
		log.Printf("Error creating request: %v", err)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
