package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultClient is a shared HTTP client with sensible defaults
var DefaultClient = &http.Client{
	Timeout: 15 * time.Second,
}

// LongTimeoutClient is for operations that may take longer
var LongTimeoutClient = &http.Client{
	Timeout: 30 * time.Second,
}

// MakeRequest performs an HTTP GET request with context and returns the response
func MakeRequest(ctx context.Context, apiURL string, client *http.Client) (*http.Response, error) {
	if client == nil {
		client = DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch results: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("provider returned status %d", resp.StatusCode)
	}

	return resp, nil
}

// BuildQueryURL builds a URL with query parameters
func BuildQueryURL(baseURL string, params map[string]string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return baseURL // Return original if parsing fails
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ReadResponseBody reads the entire response body (useful for error messages)
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// DecodeJSONResponse decodes a JSON response from an HTTP response body
func DecodeJSONResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}
