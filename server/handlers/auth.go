package handlers

import (
	"Arrgo/services"
	"html/template"
	"log"
	"log/slog"
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
			slog.Error("Error rendering register template", "error", err)
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
		slog.Error("Registration failed", "username", username, "error", err)
		http.Error(w, "Registration failed", http.StatusInternalServerError)
		return
	}

	slog.Info("User registered successfully", "username", username, "user_id", user.ID)

	// Automatically log in after registration
	if err := SetupUserSession(w, r, user); err != nil {
		slog.Error("Failed to setup session", "username", username, "error", err)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	slog.Debug("Login handler called",
		"method", r.Method,
		"path", r.URL.Path,
		"htmx_request", r.Header.Get("HX-Request"))
	if r.Method == http.MethodGet {
		if err := loginTmpl.ExecuteTemplate(w, "base", nil); err != nil {
			slog.Error("Error rendering login template", "error", err)
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

	slog.Info("Login attempt", "username", username)

	if username == "" || password == "" {
		slog.Warn("Login failed: missing credentials", "username", username)
		http.Error(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	user, err := services.AuthenticateUser(username, password)
	if err != nil {
		slog.Warn("Login failed", "username", username, "error", err)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	slog.Info("User authenticated successfully", "username", username, "user_id", user.ID)

	// Create session
	if err := SetupUserSession(w, r, user); err != nil {
		slog.Error("Failed to setup session", "username", username, "error", err)
		http.Error(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	slog.Info("Session saved, redirecting to dashboard", "username", username)

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

