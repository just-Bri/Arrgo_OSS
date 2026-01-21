package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
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
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	requests, err := services.GetRequests()
	if err != nil {
		slog.Error("Error getting requests", "error", err)
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

	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req models.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	req.UserID = int(user.ID)

	// Final server-side check to prevent duplicate requests or requesting library items
	var externalID string
	if req.MediaType == "movie" {
		externalID = req.TMDBID
	} else {
		externalID = req.TVDBID
	}

	status, err := services.CheckLibraryStatus(req.MediaType, externalID)
	if err != nil {
		slog.Error("Error checking library status", "error", err, "media_type", req.MediaType, "external_id", externalID)
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
		var duplicateSeasons []string
		
		for _, rs := range requestedSeasons {
			rs = strings.TrimSpace(rs)
			sn, err := strconv.Atoi(rs)
			if err != nil {
				continue
			}

			// Check if in library
			inLibrary := slices.Contains(status.Seasons, sn)

			// Check if already requested
			alreadyRequested := slices.Contains(status.RequestedSeasons, sn)

			if inLibrary || alreadyRequested {
				duplicateSeasons = append(duplicateSeasons, rs)
			}
		}

		// Block if any seasons are duplicates
		if len(duplicateSeasons) > 0 {
			http.Error(w, fmt.Sprintf("Season(s) %s already exist(s) or have been requested", strings.Join(duplicateSeasons, ", ")), http.StatusConflict)
			return
		}
	}

	if err := services.CreateRequest(req); err != nil {
		slog.Error("Error creating request", "error", err, "user_id", req.UserID, "title", req.Title, "media_type", req.MediaType)
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	slog.Info("Request created successfully", "user_id", req.UserID, "title", req.Title, "media_type", req.MediaType, "seasons", req.Seasons)

	// Trigger immediate processing if automation service is available
	if automation := services.GetGlobalAutomationService(); automation != nil {
		// Use background context with timeout for immediate processing
		processCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		go automation.TriggerImmediateProcessing(processCtx)
		slog.Info("Triggered immediate processing for new request", "title", req.Title)
	} else {
		slog.Warn("Automation service not available, request will be processed on next scheduled check (1 hour)")
	}

	w.WriteHeader(http.StatusCreated)
}

func ApproveRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := services.UpdateRequestStatus(id, "approved"); err != nil {
		slog.Error("Error approving request", "error", err, "request_id", id, "user", user.Username)
		http.Error(w, "Failed to approve request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func DenyRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := services.UpdateRequestStatus(id, "cancelled"); err != nil {
		slog.Error("Error denying request", "error", err, "request_id", id, "user", user.Username)
		http.Error(w, "Failed to deny request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func DeleteRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	id, err := ParseIDFromQuery(r, "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cfg := config.Load()
	qb, _ := services.NewQBittorrentClient(cfg)

	if err := services.DeleteRequest(id, qb); err != nil {
		slog.Error("Error deleting request", "error", err, "request_id", id, "user", user.Username)
		http.Error(w, "Failed to delete request", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
