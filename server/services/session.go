package services

import (
	"Arrgo/config"
	"net/http"

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

func SaveSession(w http.ResponseWriter, r *http.Request, session *sessions.Session) error {
	return session.Save(r, w)
}

