package main

import (
	"log"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

type SyncRequest struct {
	Video    string `json:"video"`
	Subtitle string `json:"subtitle"`
}

func main() {
	e := echo.New()

	e.Use(middleware.Logger())
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

		log.Printf("Starting subtitle sync for video: %s, subtitle: %s", filepath.Base(videoPath), filepath.Base(subtitlePath))

		// ffsubsync <video> -i <subtitle> -o <subtitle>
		// We use the same path for input and output, directly overwriting the file.
		cmd := exec.Command("ffsubsync", videoPath, "-i", subtitlePath, "-o", subtitlePath)
		
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
