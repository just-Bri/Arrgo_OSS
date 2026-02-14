# Migration Guide: Using the Shared Library

This guide shows how to migrate both the server and indexer apps to use the shared library.

## Shared Components Identified

### 1. **HTTP Client Utilities** (`shared/http`)
- **Current duplication**: 
  - `indexer/providers/http_client.go` - DefaultHTTPClient
  - `indexer/providers/utils.go` - MakeHTTPRequest, BuildQueryURL
  - `server/services/metadata.go` - httpClient variable
  - `server/services/subtitles.go` - Multiple http.Client creations
  - `server/services/automation.go` - httpClient field
  - `server/services/qbittorrent.go` - client field

- **Shared solution**: Use `shared/http` package

### 2. **Byte Formatting** (`shared/format`)
- **Current duplication**:
  - `indexer/providers/utils.go` - FormatBytes()
  - `server/handlers/utils.go` - formatSize template function

- **Shared solution**: Use `shared/format.Bytes()`

### 3. **Environment Variables** (`shared/config`)
- **Current duplication**:
  - `indexer/main.go` - getEnv()
  - `server/config/config.go` - getEnv()

- **Shared solution**: Use `shared/config.GetEnv()`

### 4. **Logging Middleware** (`shared/middleware`)
- **Current duplication**:
  - `indexer/main.go` - loggingMiddleware()
  - `server/main.go` - loggingMiddleware()

- **Shared solution**: Use `shared/middleware.Logging()` or `LoggingSimple()`

### 5. **Server Configuration** (`shared/server`)
- **Current duplication**:
  - Both apps create http.Server with similar timeout configurations

- **Shared solution**: Use `shared/server.CreateServer()` with `DefaultConfig()`

## Migration Steps

### Step 1: Add Shared Library to go.mod

**Server (`server/go.mod`)**:
```go
require github.com/justbri/arrgo/shared v0.0.0

replace github.com/justbri/arrgo/shared => ../shared
```

**Indexer (`indexer/go.mod`)**:
```go
require github.com/justbri/arrgo/shared v0.0.0

replace github.com/justbri/arrgo/shared => ../shared
```

### Step 2: Update Server App

**server/config/config.go**:
```go
import "github.com/justbri/arrgo/shared/config"

// Replace getEnv() calls with:
config.GetEnv("DATABASE_URL", "postgres://...")
```

**server/main.go**:
```go
import (
    "github.com/justbri/arrgo/shared/middleware"
    "github.com/justbri/arrgo/shared/server"
)

// Replace loggingMiddleware with:
Handler: middleware.Logging(mux)

// Replace server creation with:
srv := server.CreateServer(server.DefaultConfig(":"+cfg.ServerPort), loggingMiddleware(mux))
```

**server/handlers/utils.go**:
```go
import "github.com/justbri/arrgo/shared/format"

// Replace formatSize function with:
"formatSize": format.Bytes,
```

**server/services/metadata.go**:
```go
import sharedhttp "github.com/justbri/arrgo/shared/http"

// Replace httpClient variable with:
var httpClient = sharedhttp.DefaultClient

// Replace http.Get() calls with:
resp, err := sharedhttp.MakeRequest(ctx, url, sharedhttp.DefaultClient)
```

### Step 3: Update Indexer App

**indexer/main.go**:
```go
import (
    "github.com/justbri/arrgo/shared/config"
    "github.com/justbri/arrgo/shared/middleware"
    "github.com/justbri/arrgo/shared/server"
)

// Replace getEnv with:
port := config.GetEnv("PORT", "5004")

// Replace loggingMiddleware with:
Handler: middleware.LoggingSimple(mux)

// Replace server creation with:
srv := server.CreateServer(server.DefaultConfig(":"+port), middleware.LoggingSimple(mux))
```

**indexer/providers/utils.go**:
```go
import (
    sharedhttp "github.com/justbri/arrgo/shared/http"
    "github.com/justbri/arrgo/shared/format"
)

// Remove MakeHTTPRequest, BuildQueryURL, FormatBytes
// Use sharedhttp.MakeRequest, sharedhttp.BuildQueryURL, format.Bytes instead
```

**indexer/providers/http_client.go**:
```go
// Delete this file, use sharedhttp.DefaultClient instead
```

**indexer/providers/yts.go** and **solid.go**:
```go
import sharedhttp "github.com/justbri/arrgo/shared/http"

// Replace MakeHTTPRequest with:
resp, err := sharedhttp.MakeRequest(ctx, apiURL, sharedhttp.DefaultClient)

// Replace BuildQueryURL with:
apiURL := sharedhttp.BuildQueryURL("https://...", map[string]string{...})
```

## Benefits

1. **Consistency**: Both apps use the same HTTP client configuration
2. **Maintainability**: Fix bugs or improve utilities in one place
3. **DRY Principle**: Eliminates ~200+ lines of duplicate code
4. **Testing**: Shared utilities can be tested once
5. **Future-proof**: Easy to add new shared utilities

## Estimated Code Reduction

- **Server**: ~100 lines removed
- **Indexer**: ~80 lines removed
- **Total**: ~180 lines of duplicate code eliminated
