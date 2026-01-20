package providers

import (
	"context"
	"net/http"

	sharedhttp "github.com/justbri/arrgo/shared/http"
	"github.com/justbri/arrgo/shared/format"
)

// FormatBytes formats bytes into human-readable format (KB, MB, GB, etc.)
// Deprecated: Use shared/format.Bytes instead
var FormatBytes = format.Bytes

// MakeHTTPRequest performs an HTTP GET request with context and returns the response body
// Deprecated: Use shared/http.MakeRequest instead
func MakeHTTPRequest(ctx context.Context, apiURL string, client *http.Client) (*http.Response, error) {
	return sharedhttp.MakeRequest(ctx, apiURL, client)
}

// DecodeJSONResponse decodes a JSON response from an HTTP response body
// Deprecated: Use shared/http.DecodeJSONResponse instead
func DecodeJSONResponse(resp *http.Response, v interface{}) error {
	return sharedhttp.DecodeJSONResponse(resp, v)
}

// BuildQueryURL builds a URL with query parameters
// Deprecated: Use shared/http.BuildQueryURL instead
func BuildQueryURL(baseURL string, params map[string]string) string {
	return sharedhttp.BuildQueryURL(baseURL, params)
}

// ReadResponseBody reads the entire response body (useful for error messages)
// Deprecated: Use shared/http.ReadResponseBody instead
func ReadResponseBody(resp *http.Response) ([]byte, error) {
	return sharedhttp.ReadResponseBody(resp)
}
