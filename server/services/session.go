package services

import (
	"Arrgo/config"
	"net/http"
	"strings"

	"github.com/gorilla/sessions"
)

var store *sessions.CookieStore

func InitSessionStore(cfg *config.Config) {
	store = sessions.NewCookieStore([]byte(cfg.SessionSecret))

	secure := false
	if cfg.Environment == "production" {
		secure = true
	}

	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func GetSession(r *http.Request) (*sessions.Session, error) {
	return store.Get(r, "arrgo-session")
}

// GetOrCreateSession gets an existing session or creates a new one if the existing one is invalid
func GetOrCreateSession(w http.ResponseWriter, r *http.Request) (*sessions.Session, error) {
	session, err := store.Get(r, "arrgo-session")
	if err != nil {
		// If the cookie exists but can't be decoded (e.g., encrypted with old secret),
		// clear it and create a new session
		if strings.Contains(err.Error(), "securecookie: the value is not valid") {
			// Clear the invalid cookie
			cookie := &http.Cookie{
				Name:     "arrgo-session",
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
			}
			http.SetCookie(w, cookie)
			// Return a new session
			return store.New(r, "arrgo-session"), nil
		}
		return nil, err
	}
	return session, nil
}

func SaveSession(w http.ResponseWriter, r *http.Request, session *sessions.Session) error {
	return session.Save(r, w)
}
