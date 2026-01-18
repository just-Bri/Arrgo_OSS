package handlers

import (
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
)

var adminTmpl *template.Template

func init() {
	var err error
	funcMap := GetFuncMap()
	adminTmpl, err = template.New("admin").Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/admin.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		log.Fatal("Failed to parse admin template:", err)
	}
}

type AdminPageData struct {
	Username    string
	IsAdmin     bool
	SearchQuery string
}

func AdminHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	if !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	data := AdminPageData{
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		SearchQuery: "",
	}

	if err := adminTmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Error rendering admin template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
