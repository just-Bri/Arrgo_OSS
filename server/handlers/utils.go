package handlers

import (
	"Arrgo/config"
	"Arrgo/models"
	"Arrgo/services"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"slices"

	"github.com/justbri/arrgo/shared/format"
)

func interfaceToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	default:
		return 0
	}
}

func GetFuncMap() template.FuncMap {
	return template.FuncMap{
		"hasPrefix": strings.HasPrefix,
		"split":     strings.Split,
		"contains":  strings.Contains,
		"containsInt": func(slice []int, val int) bool {
			return slices.Contains(slice, val)
		},
		"formatSize": format.Bytes,
		"title": func(s string) string {
			if len(s) == 0 {
				return s
			}
			return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
		},
	}
}

// ExtractGenresFromMovies extracts unique genres from a slice of movies
func ExtractGenresFromMovies(movies []models.Movie) []string {
	genreMap := make(map[string]bool)
	for _, m := range movies {
		if m.Genres != "" {
			gs := strings.SplitSeq(m.Genres, ", ")
			for g := range gs {
				if g != "" {
					genreMap[g] = true
				}
			}
		}
	}
	var allGenres []string
	for g := range genreMap {
		allGenres = append(allGenres, g)
	}
	sort.Strings(allGenres)
	return allGenres
}

// ExtractGenresFromShows extracts unique genres from a slice of shows
func ExtractGenresFromShows(shows []models.Show) []string {
	genreMap := make(map[string]bool)
	for _, s := range shows {
		if s.Genres != "" {
			gs := strings.SplitSeq(s.Genres, ", ")
			for g := range gs {
				if g != "" {
					genreMap[g] = true
				}
			}
		}
	}
	var allGenres []string
	for g := range genreMap {
		allGenres = append(allGenres, g)
	}
	sort.Strings(allGenres)
	return allGenres
}

// ExtractYearsFromMovies extracts unique years from a slice of movies, sorted descending
func ExtractYearsFromMovies(movies []models.Movie) []int {
	yearMap := make(map[int]bool)
	for _, m := range movies {
		if m.Year > 0 {
			yearMap[m.Year] = true
		}
	}
	var years []int
	for y := range yearMap {
		years = append(years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years
}

// ExtractYearsFromShows extracts unique years from a slice of shows, sorted descending
func ExtractYearsFromShows(shows []models.Show) []int {
	yearMap := make(map[int]bool)
	for _, s := range shows {
		if s.Year > 0 {
			yearMap[s.Year] = true
		}
	}
	var years []int
	for y := range yearMap {
		years = append(years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	return years
}

// ExtractQualitiesFromMovies extracts unique qualities from a slice of movies
func ExtractQualitiesFromMovies(movies []models.Movie) []string {
	qualityMap := make(map[string]bool)
	for _, m := range movies {
		if m.Quality != "" {
			qualityMap[m.Quality] = true
		}
	}
	var qualities []string
	for q := range qualityMap {
		qualities = append(qualities, q)
	}
	sort.Strings(qualities)
	return qualities
}

// ExtractStatusesFromMovies extracts unique statuses from a slice of movies
func ExtractStatusesFromMovies(movies []models.Movie) []string {
	statusMap := make(map[string]bool)
	for _, m := range movies {
		if m.Status != "" {
			statusMap[m.Status] = true
		}
	}
	var statuses []string
	for s := range statusMap {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	return statuses
}

// ExtractStatusesFromShows extracts unique statuses from a slice of shows
func ExtractStatusesFromShows(shows []models.Show) []string {
	statusMap := make(map[string]bool)
	for _, s := range shows {
		if s.Status != "" {
			statusMap[s.Status] = true
		}
	}
	var statuses []string
	for s := range statusMap {
		statuses = append(statuses, s)
	}
	sort.Strings(statuses)
	return statuses
}

// SeparateIncomingMovies separates incoming and library movies based on path prefixes
func SeparateIncomingMovies(allMovies []models.Movie, cfg *config.Config, isAdmin bool) (libraryMovies []models.Movie, incomingMovies []models.Movie) {
	for _, m := range allMovies {
		isIncoming := strings.HasPrefix(m.Path, cfg.IncomingMoviesPath)
		if isIncoming {
			if isAdmin {
				incomingMovies = append(incomingMovies, m)
			}
		} else {
			libraryMovies = append(libraryMovies, m)
		}
	}
	return libraryMovies, incomingMovies
}

// SeparateIncomingShows separates incoming and library shows based on path prefixes
func SeparateIncomingShows(allShows []models.Show, cfg *config.Config, isAdmin bool) (libraryShows []models.Show, incomingShows []models.Show) {
	for _, s := range allShows {
		isIncoming := strings.HasPrefix(s.Path, cfg.IncomingShowsPath)
		if isIncoming {
			if isAdmin {
				incomingShows = append(incomingShows, s)
			}
		} else {
			libraryShows = append(libraryShows, s)
		}
	}
	return libraryShows, incomingShows
}

// RequireAdmin checks if the current user is an admin, returns error if not
func RequireAdmin(r *http.Request) (*models.User, error) {
	user, err := GetCurrentUser(r)
	if err != nil || user == nil {
		return nil, fmt.Errorf("unauthorized")
	}
	if !user.IsAdmin {
		return nil, fmt.Errorf("forbidden")
	}
	return user, nil
}

// RequireAdminHandler wraps a handler to require admin access
func RequireAdminHandler(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, err := RequireAdmin(r)
		if err != nil {
			if err.Error() == "unauthorized" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
			} else {
				http.Error(w, "Forbidden", http.StatusForbidden)
			}
			return
		}
		handler(w, r)
	}
}

// RequirePostMethod validates that the request method is POST
func RequirePostMethod(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handler(w, r)
	}
}

// ParseIDFromQuery extracts and parses an integer ID from query parameters
func ParseIDFromQuery(r *http.Request, param string) (int, error) {
	idStr := r.URL.Query().Get(param)
	if idStr == "" {
		return 0, fmt.Errorf("missing %s parameter", param)
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("invalid %s parameter", param)
	}
	return id, nil
}

// SetupUserSession creates a session for a user after login/registration
func SetupUserSession(w http.ResponseWriter, r *http.Request, user *models.User) error {
	session, err := services.GetSession(r)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username

	if err := services.SaveSession(w, r, session); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// LoadTemplate loads a template with common files and function map
func LoadTemplate(name string, pageTemplate string, useFuncMap bool) (*template.Template, error) {
	var funcMap template.FuncMap
	if useFuncMap {
		funcMap = GetFuncMap()
	} else {
		funcMap = template.FuncMap{}
	}

	tmpl, err := template.New(name).Funcs(funcMap).ParseFiles(
		"templates/layouts/base.html",
		pageTemplate,
		"templates/components/navigation.html",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse template %s: %w", name, err)
	}

	return tmpl, nil
}
