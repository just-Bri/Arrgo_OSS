package middleware

import (
	"Arrgo/services"
	"net/http"
	"strconv"
)

func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := services.GetSession(r)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		userID, ok := session.Values["user_id"]
		if !ok {
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
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
		default:
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		_, err = services.GetUserByID(userIDInt)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

