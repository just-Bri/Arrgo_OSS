# Migration Guide: Using the Shared Library

## Status: Complete

All migrations described below have been applied. This document records what was done and why.

## What Was Migrated

### 1. HTTP Client Utilities → `shared/http`
**Removed from:**
- `indexer/providers/http_client.go` (deleted)
- `indexer/providers/utils.go` (deleted)
- Inline `http.Client` constructions in server services

**Use instead:**
```go
import sharedhttp "github.com/justbri/arrgo/shared/http"

resp, err := sharedhttp.MakeRequest(ctx, url, sharedhttp.DefaultClient)
apiURL := sharedhttp.BuildQueryURL("https://...", map[string]string{"q": query})
```

### 2. Byte Formatting → `shared/format`
**Removed from:** `indexer/providers/utils.go`, `server/handlers/utils.go`

**Use instead:**
```go
import "github.com/justbri/arrgo/shared/format"

sizeStr := format.Bytes(fileSize)
```

### 3. Environment Variables → `shared/config`
**Removed from:** `indexer/main.go` (local `getEnv`), `server/config/config.go`

**Use instead:**
```go
import "github.com/justbri/arrgo/shared/config"

port := config.GetEnv("PORT", "5004")
```

### 4. Logging Middleware → `shared/middleware`
**Removed from:** Inline `loggingMiddleware` functions in both apps.

Note: `LoggingSimple` was removed — both apps use `sharedmiddleware.Logging`.

**Use:**
```go
import sharedmiddleware "github.com/justbri/arrgo/shared/middleware"

r.Use(sharedmiddleware.Logging)
```

### 5. Server Configuration → `shared/server`
**Use:**
```go
import "github.com/justbri/arrgo/shared/server"

srvConfig := server.DefaultConfig(":" + port)
srv := server.CreateServer(srvConfig, mux)
```

### 6. Torrent Indexers → `shared/indexers`
**Removed from:**
- `server/services/indexers/` (all files deleted)
- `indexer/providers/` (all files deleted)

**Use instead:**
```go
import sharedindexers "github.com/justbri/arrgo/shared/indexers"

for _, idx := range sharedindexers.Indexers() {
    results, _ := idx.SearchMovies(ctx, query)
}

// Periodic Nyaa cache cleanup:
sharedindexers.CleanupNyaaCache()
```

## go.mod Setup

Each service that uses `shared/indexers` (or any shared package) needs:

```
replace github.com/justbri/arrgo/shared => ../shared
require github.com/justbri/arrgo/shared v0.0.0-00010101000000-000000000000
```

The `shared` module itself requires `golang.org/x/net` (for 1337x HTML parsing and Nyaa HTTP).
