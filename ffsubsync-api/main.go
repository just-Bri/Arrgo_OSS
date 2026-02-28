package main

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync" // Added for mutex

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type SyncRequest struct {
	Video    string `json:"video"`
	Subtitle string `json:"subtitle"`
}

var (
	// syncMutex ensures only one ffsubsync process runs at a time to prevent CPU exhaustion.
	syncMutex sync.Mutex

	moviesPath string
	showsPath  string
)

func isPathAllowed(path string) bool {
	// If paths aren't set, allow all (backward compatibility/default behavior)
	if moviesPath == "" && showsPath == "" {
		return true
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	if moviesPath != "" && strings.HasPrefix(absPath, moviesPath) {
		return true
	}
	if showsPath != "" && strings.HasPrefix(absPath, showsPath) {
		return true
	}

	return false
}

func main() {
	moviesPath = os.Getenv("MOVIES_PATH")
	showsPath = os.Getenv("SHOWS_PATH")

	log.Printf("SubSync API starting...")
	log.Printf("Allowed Movies Path: %s", moviesPath)
	log.Printf("Allowed Shows Path: %s", showsPath)

	e := echo.New()

	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogMethod:   true,
		LogLatency:  true,
		LogError:    true,
		LogRemoteIP: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error != nil {
				log.Printf("ERROR: %s %s %s %d %s | error: %v", v.RemoteIP, v.Method, v.URI, v.Status, v.Latency, v.Error)
			} else {
				log.Printf("REQUEST: %s %s %s %d %s", v.RemoteIP, v.Method, v.URI, v.Status, v.Latency)
			}
			return nil
		},
	}))
	e.Use(middleware.Recover())

	e.POST("/sync", func(c echo.Context) error {
		req := new(SyncRequest)
		if err := c.Bind(req); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid JSON format"})
		}

		if req.Video == "" || req.Subtitle == "" {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "Missing video or subtitle path"})
		}

		// Use the absolute path provided in the request as they are mapped in the same way
		// between Arrgo and this container via the shared media volume.
		videoPath := req.Video
		subtitlePath := req.Subtitle

		if !isPathAllowed(videoPath) || !isPathAllowed(subtitlePath) {
			log.Printf("Access denied for paths outside of allowed directories: %s, %s", videoPath, subtitlePath)
			return c.JSON(http.StatusForbidden, map[string]string{"error": "Access denied: paths must be within MOVIES_PATH or SHOWS_PATH"})
		}

		// Acquire lock to serialize sync tasks
		syncMutex.Lock()
		defer syncMutex.Unlock()

		log.Printf("Starting subtitle sync for video: %s, subtitle: %s", filepath.Base(videoPath), filepath.Base(subtitlePath))

		// ffsubsync <video> -i <subtitle> -o <subtitle>
		// We use the same path for input and output, directly overwriting the file.
		// Wrapped in 'nice -n 15' to give it lower CPU priority.
		cmd := exec.Command("nice", "-n", "15", "ffsubsync", videoPath, "-i", subtitlePath, "-o", subtitlePath)

		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("ffsubsync failed: %v\nOutput: %s", err, string(output))
			return c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error":   "Failed to process subtitle",
				"details": string(output),
			})
		}

		log.Printf("Successfully synced subtitle: %s", filepath.Base(subtitlePath))
		return c.JSON(http.StatusOK, map[string]string{"message": "success"})
	})

	e.GET("/health", func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	})

	// Start server
	e.Logger.Fatal(e.Start(":8080"))
}
