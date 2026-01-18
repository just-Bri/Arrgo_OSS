package handlers

import (
	"Arrgo/services"
	"html/template"
	"log"
	"net/http"
)

var loginTmpl *template.Template
var registerTmpl *template.Template

func init() {
	var err error
	loginTmpl, err = template.ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/login.html",
	)
	if err != nil {
		log.Fatal("Failed to parse login template:", err)
	}

	registerTmpl, err = template.ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/register.html",
	)
	if err != nil {
		log.Fatal("Failed to parse register template:", err)
	}
}

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if err := registerTmpl.ExecuteTemplate(w, "base", nil); err != nil {
			log.Printf("Error rendering register template: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")

	if username == "" || email == "" || password == "" {
		http.Error(w, "Username, email and password are required", http.StatusBadRequest)
		return
	}

	user, err := services.RegisterUser(username, email, password)
	if err != nil {
		log.Printf("Registration failed for user %s: %v", username, err)
		http.Error(w, "Registration failed", http.StatusInternalServerError)
		return
	}

	log.Printf("User %s registered successfully, ID: %d", username, user.ID)

	// Automatically log in after registration
	session, err := services.GetSession(r)
	if err != nil {
		log.Printf("Failed to get session: %v", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username

	if err := services.SaveSession(w, r, session); err != nil {
		log.Printf("Failed to save session: %v", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("LoginHandler: %s %s (HX-Request: %s)", r.Method, r.URL.Path, r.Header.Get("HX-Request"))
	if r.Method == http.MethodGet {
		if err := loginTmpl.ExecuteTemplate(w, "base", nil); err != nil {
			log.Printf("Error rendering login template: %v", err)
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

	log.Printf("Attempting login for user: %s", username)

	if username == "" || password == "" {
		log.Printf("Login failed: missing credentials")
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	user, err := services.AuthenticateUser(username, password)
	if err != nil {
		log.Printf("Login failed for user %s: %v", username, err)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	log.Printf("User %s authenticated successfully, ID: %d", username, user.ID)

	// Create session
	session, err := services.GetSession(r)
	if err != nil {
		log.Printf("Failed to get session: %v", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username

	if err := services.SaveSession(w, r, session); err != nil {
		log.Printf("Failed to save session: %v", err)
		http.Error(w, "Failed to save session", http.StatusInternalServerError)
		return
	}

	log.Printf("Session saved for user %s, redirecting to dashboard", username)

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

