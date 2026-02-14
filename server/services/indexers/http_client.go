package indexers

import (
	sharedhttp "github.com/justbri/arrgo/shared/http"
)

// DefaultHTTPClient is a shared HTTP client with timeout for all providers
// Deprecated: Use sharedhttp.DefaultClient instead
var DefaultHTTPClient = sharedhttp.DefaultClient
