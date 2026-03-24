package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/justbri/arrgo/shared/config"
)

// FlareSolverrResponse represents the response from FlareSolverr
type FlareSolverrResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Solution struct {
		URL      string        `json:"url"`
		Response string        `json:"response"`
		Cookies  []interface{} `json:"cookies"`
	} `json:"solution"`
}

// FetchViaBypass uses the FlareSolverr service to bypass Cloudflare challenges
func FetchViaBypass(ctx context.Context, targetURL string) (string, error) {
	bypassURL := config.GetEnv("CLOUDFLARE_BYPASS_URL", "")
	if bypassURL == "" {
		return "", fmt.Errorf("CLOUDFLARE_BYPASS_URL not configured")
	}

	// Ensure no trailing slash
	bypassURL = strings.TrimSuffix(bypassURL, "/")

	// FlareSolverr-compatible API format
	// POST to /v1 with JSON body
	requestBody := map[string]interface{}{
		"cmd":        "request.get",
		"url":        targetURL,
		"maxTimeout": 60000,
		"wait":       10000, // Wait 10 seconds for JS to render
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal FlareSolverr request: %w", err)
	}

	slog.Debug("Calling FlareSolverr bypass service", "url", targetURL, "bypass_url", bypassURL)

	// Make POST request to bypass service
	req, err := http.NewRequestWithContext(ctx, "POST", bypassURL+"/v1", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create FlareSolverr request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Use a longer timeout client for FlareSolverr requests
	client := &http.Client{
		Timeout: 90 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to call bypass service: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read bypass response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bypass service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var bypassResp FlareSolverrResponse
	if err := json.Unmarshal(bodyBytes, &bypassResp); err != nil {
		return "", fmt.Errorf("failed to decode bypass response: %w, body: %s", err, string(bodyBytes))
	}

	if bypassResp.Status != "ok" {
		return "", fmt.Errorf("bypass service returned status %s: %s", bypassResp.Status, bypassResp.Message)
	}

	if bypassResp.Solution.Response == "" {
		return "", fmt.Errorf("bypass service returned empty response")
	}

	// Log a warning if the response seems too short for a search/torrent page
	// Most pages are at least 5-10KB. A 900-byte response is likely a challenge page that FlareSolverr let through.
	if len(bypassResp.Solution.Response) < 2000 {
		slog.Warn("Bypass service returned suspiciously short content",
			"url", targetURL,
			"length", len(bypassResp.Solution.Response),
			"short_content", bypassResp.Solution.Response)
	}

	return bypassResp.Solution.Response, nil
}
