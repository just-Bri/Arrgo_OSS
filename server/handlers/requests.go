package handlers

import (
	"Arrgo/models"
	"Arrgo/services"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strconv"
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
	Username    string
	IsAdmin     bool
	CurrentPage string
	SearchQuery string
	Requests    []models.Request
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
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		CurrentPage: "/requests",
		SearchQuery: "",
		Requests:    requests,
	}

	if err := requestsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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

	if req.MediaType == "movie" {
		if status.Exists || strings.Contains(status.Message, "Already requested") {
			http.Error(w, "Movie already exists or has been requested", http.StatusConflict)
			return
		}
	} else if req.MediaType == "show" {
		// For shows, we check if the requested seasons are already in library or already requested
		requestedSeasons := strings.Split(req.Seasons, ",")
		allNew := true
		for _, rs := range requestedSeasons {
			sn, _ := strconv.Atoi(strings.TrimSpace(rs))
			
			// Check if in library
			inLibrary := false
			for _, s := range status.Seasons {
				if s == sn {
					inLibrary = true
					break
				}
			}
			
			// Check if already requested
			alreadyRequested := false
			for _, s := range status.RequestedSeasons {
				if s == sn {
					alreadyRequested = true
					break
				}
			}
			
			if inLibrary || alreadyRequested {
				allNew = false
				break
			}
		}
		
		if !allNew && len(requestedSeasons) == 1 {
			http.Error(w, "Season already exists or has been requested", http.StatusConflict)
			return
		}
	}

	if err := services.CreateRequest(req); err != nil {
		log.Printf("Error creating request: %v", err)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
