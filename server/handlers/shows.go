package handlers

import (
	"Arrgo/models"
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
	"strings"
)

var showsTmpl *template.Template

func init() {
	var err error
	funcMap := template.FuncMap{
		"hasPrefix": strings.HasPrefix,
	}
	showsTmpl, err = template.New("shows").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/shows.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse shows template:", err)
	}
}

type ShowsData struct {
	Username string
	Shows    []models.Show
}

func ShowsHandler(w http.ResponseWriter, r *http.Request) {
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

	shows, err := services.GetShows()
	if err != nil {
		log.Printf("Error getting shows: %v", err)
		shows = []models.Show{}
	}

	data := ShowsData{
		Username: user.Username,
		Shows:    shows,
	}

	if err := showsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
