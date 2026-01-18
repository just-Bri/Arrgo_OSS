package handlers

import (
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
)

var loginTmpl *template.Template

func init() {
	var err error
	loginTmpl, err = template.ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/login.html",
	)
	if err != nil {
		log.Fatal("Failed to parse login template:", err)
	}
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if err := loginTmpl.ExecuteTemplate(w, "base", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	user, err := services.AuthenticateUser(username, password)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Create session
	session, err := services.GetSession(r)
	if err != nil {
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username

	if err := services.SaveSession(w, r, session); err != nil {
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	// Check if HTMX request
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/dashboard")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	session, err := services.GetSession(r)
	if err == nil {
		session.Values = make(map[interface{}]interface{})
		session.Options.MaxAge = -1
		services.SaveSession(w, r, session)
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

