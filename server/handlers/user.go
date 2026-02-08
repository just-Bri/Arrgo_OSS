package handlers

import (
	"Arrgo/models"
	"Arrgo/services"
	"log/slog"
	"net/http"
	"strconv"
)

func GetCurrentUser(r *http.Request) (*models.User, error) {
	session, err := services.GetSession(r)
	if err != nil {
		return nil, err
	}

	userID, ok := session.Values["user_id"]
	if !ok {
		return nil, nil
	}

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
			return nil, err
		}
	default:
		return nil, nil
	}

	user, err := services.GetUserByID(userIDInt)
	if err != nil {
		slog.Error("Failed to get user info", "user_id", userIDInt, "error", err)
		return nil, err
	}

	return user, nil
}
