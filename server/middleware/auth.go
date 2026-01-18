package middleware

import (
	"Arrgo/services"
	"log"
	"net/http"
	"strconv"
)

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := services.GetSession(r)
		if err != nil {
			log.Printf("[AUTH] No session found for %s, redirecting to /login", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		userID, ok := session.Values["user_id"]
		if !ok {
			log.Printf("[AUTH] User not authenticated for %s, redirecting to /login", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Verify user still exists
		var userIDInt int64
		switch v := userID.(type) {
		case int64:
			userIDInt = v
		case int:
			userIDInt = int64(v)
		case string:
			var err error
			userIDInt, err = strconv.ParseInt(v, 10, 64)
			if err != nil {
				log.Printf("[AUTH] Invalid user_id in session for %s, redirecting to /login", r.URL.Path)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		default:
			log.Printf("[AUTH] Unknown user_id type in session for %s, redirecting to /login", r.URL.Path)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		_, err = services.GetUserByID(userIDInt)
		if err != nil {
			log.Printf("[AUTH] User ID %d not found in database, redirecting to /login", userIDInt)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

