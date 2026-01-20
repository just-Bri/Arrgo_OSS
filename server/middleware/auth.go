package middleware

import (
	"Arrgo/services"
	"log"
	"net/http"
	"strconv"
)

// redirectToLogin logs the reason and redirects to login page
func redirectToLogin(w http.ResponseWriter, r *http.Request, reason string) {
	log.Printf("[AUTH] %s for %s, redirecting to /login", reason, r.URL.Path)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// parseUserID converts various userID types to int64
func parseUserID(userID interface{}) (int64, error) {
	switch v := userID.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, strconv.ErrSyntax
	}
}

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := services.GetSession(r)
		if err != nil {
			redirectToLogin(w, r, "No session found")
			return
		}

		userID, ok := session.Values["user_id"]
		if !ok {
			redirectToLogin(w, r, "User not authenticated")
			return
		}

		// Parse user ID
		userIDInt, err := parseUserID(userID)
		if err != nil {
			redirectToLogin(w, r, "Invalid user_id in session")
			return
		}

		// Verify user still exists
		_, err = services.GetUserByID(userIDInt)
		if err != nil {
			log.Printf("[AUTH] User ID %d not found in database, redirecting to /login", userIDInt)
			redirectToLogin(w, r, "User not found in database")
			return
		}

		next.ServeHTTP(w, r)
	})
}

