package handlers

import (
	"Arrgo/services"
	"html/template"
	"log/slog"
	"net/http"
	"os"
)

var settingsTmpl *template.Template

func init() {
	var err error
	settingsTmpl, err = template.ParseFiles(
		"templates/layouts/base.html",
		"templates/pages/settings.html",
		"templates/components/navigation.html",
	)
	if err != nil {
		slog.Error("Failed to parse settings template", "error", err)
		os.Exit(1)
	}
}

type SettingsData struct {
	Username    string
	IsAdmin     bool
	CurrentPage string
	SearchQuery string
	Error       string
	Success     string
}

func SettingsHandler(w http.ResponseWriter, r *http.Request) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	data := SettingsData{
		Username:    user.Username,
		IsAdmin:     user.IsAdmin,
		CurrentPage: "/settings",
	}

	if r.Method == http.MethodPost {
		currentPassword := r.FormValue("current_password")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("confirm_password")

		if newPassword != confirmPassword {
			data.Error = "New passwords do not match"
		} else if len(newPassword) < 8 {
			data.Error = "New password must be at least 8 characters"
		} else if err := services.ChangePassword(user.ID, currentPassword, newPassword); err != nil {
			slog.Warn("Password change failed", "username", user.Username, "error", err)
			data.Error = err.Error()
		} else {
			slog.Info("Password changed successfully", "username", user.Username)
			data.Success = "Password changed successfully"
		}
	}

	if err := settingsTmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("Error rendering settings template", "error", err)
	}
}
