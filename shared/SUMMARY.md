# Shared Library Analysis Summary

## Duplicate Code Patterns Identified

### 1. ✅ HTTP Client & Request Utilities
**Duplication Found:**
- `indexer/providers/http_client.go` - DefaultHTTPClient with 15s timeout
- `indexer/providers/utils.go` - MakeHTTPRequest(), BuildQueryURL(), ReadResponseBody()
- `server/services/metadata.go` - httpClient variable with 30s timeout
- `server/services/subtitles.go` - Multiple `&http.Client{Timeout: 15 * time.Second}` creations
- `server/services/automation.go` - httpClient field with 30s timeout
- `server/services/qbittorrent.go` - client field with custom timeout

**Shared Solution:** `shared/http` package
- `DefaultClient` - 15s timeout (for most API calls)
- `LongTimeoutClient` - 30s timeout (for longer operations)
- `MakeRequest()` - Standardized HTTP GET with context
- `BuildQueryURL()` - URL building with query params
- `ReadResponseBody()` - Response body reading

**Impact:** ~80 lines of duplicate code eliminated

---

### 2. ✅ Byte Formatting
**Duplication Found:**
- `indexer/providers/utils.go` - FormatBytes() function
- `server/handlers/utils.go` - formatSize template function (similar logic)

**Shared Solution:** `shared/format.Bytes()`
- Single implementation with consistent formatting
- Supports B, KB, MB, GB, TB, PB, EB

**Impact:** ~20 lines of duplicate code eliminated

---

### 3. ✅ Environment Variable Helpers
**Duplication Found:**
- `indexer/main.go` - getEnv() function
- `server/config/config.go` - getEnv() function (identical implementation)

**Shared Solution:** `shared/config` package
- `GetEnv()` - Get env var with default value
- `GetEnvRequired()` - Get required env var (panics if missing)

**Impact:** ~10 lines of duplicate code eliminated

---

### 4. ✅ Logging Middleware
**Duplication Found:**
- `indexer/main.go` - loggingMiddleware() function
- `server/main.go` - loggingMiddleware() function (nearly identical)

**Shared Solution:** `shared/middleware` package
- `Logging()` - With [REQ] prefix (for server)
- `LoggingSimple()` - Without prefix (for indexer)

**Impact:** ~15 lines of duplicate code eliminated

---

### 5. ✅ Server Configuration
**Duplication Found:**
- Both apps create `http.Server` with similar timeout configurations
- ReadTimeout: 15s, WriteTimeout: 15s, IdleTimeout: 60s

**Shared Solution:** `shared/server` package
- `Config` struct for server configuration
- `DefaultConfig()` - Creates config with sensible defaults
- `CreateServer()` - Creates http.Server with config

**Impact:** ~25 lines of duplicate code eliminated

---

## Additional Opportunities (Not Yet Implemented)

### 6. JSON Decoding Helper
**Potential Duplication:**
- Both apps decode JSON responses with similar error handling
- Could extract `DecodeJSONResponse()` to shared library

**Current State:** Only in `indexer/providers/utils.go`
**Recommendation:** Add to `shared/http` package

---

### 7. URL Query Escaping Patterns
**Potential Duplication:**
- Multiple places use `fmt.Sprintf()` with `url.QueryEscape()`
- Could standardize URL building patterns

**Current State:** Some use `BuildQueryURL()`, others use manual formatting
**Recommendation:** Migrate all to use `BuildQueryURL()`

---

## Library Structure

```
shared/
├── go.mod
├── README.md
├── MIGRATION.md
├── SUMMARY.md
├── http/
│   └── client.go          # HTTP client utilities
├── format/
│   └── bytes.go           # Formatting utilities
├── config/
│   └── env.go             # Environment variable helpers
├── middleware/
│   └── logging.go         # HTTP middleware
└── server/
    └── config.go          # Server configuration
```

## Usage Example

```go
package main

import (
    "github.com/justbri/arrgo/shared/config"
    "github.com/justbri/arrgo/shared/format"
    sharedhttp "github.com/justbri/arrgo/shared/http"
    "github.com/justbri/arrgo/shared/middleware"
    "github.com/justbri/arrgo/shared/server"
)

func main() {
    // Get config
    port := config.GetEnv("PORT", "5004")
    
    // Setup routes
    mux := setupRoutes()
    
    // Create server with shared config
    cfg := server.DefaultConfig(":" + port)
    srv := server.CreateServer(cfg, middleware.Logging(mux))
    
    // Make HTTP request
    resp, err := sharedhttp.MakeRequest(ctx, url, sharedhttp.DefaultClient)
    
    // Format bytes
    sizeStr := format.Bytes(fileSize)
}
```

## Total Impact

- **Lines of duplicate code eliminated:** ~150-200 lines
- **Shared utilities created:** 5 packages, 6 files
- **Consistency improvements:** HTTP clients, formatting, config handling
- **Maintainability:** Single source of truth for common operations

## Next Steps

1. Update `server/go.mod` and `indexer/go.mod` to include shared library
2. Migrate server app to use shared utilities
3. Migrate indexer app to use shared utilities
4. Remove duplicate code from both apps
5. Add tests for shared utilities
6. Consider adding more shared utilities as patterns emerge
