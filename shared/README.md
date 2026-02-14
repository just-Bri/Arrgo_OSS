# Arrgo Shared Library

Common utilities shared between the server and indexer applications.

## Packages

### `http`
HTTP client utilities and request helpers:
- `DefaultClient` - Shared HTTP client with 15s timeout
- `LongTimeoutClient` - HTTP client with 30s timeout
- `MakeRequest()` - Perform HTTP GET requests with context
- `BuildQueryURL()` - Build URLs with query parameters
- `ReadResponseBody()` - Read response body for error messages

### `format`
Formatting utilities:
- `Bytes()` - Format bytes into human-readable format (KB, MB, GB, etc.)

### `config`
Configuration utilities:
- `GetEnv()` - Get environment variable with default value
- `GetEnvRequired()` - Get required environment variable (panics if missing)

### `middleware`
HTTP middleware:
- `Logging()` - Request logging middleware with [REQ] prefix
- `LoggingSimple()` - Simple request logging without prefix

### `server`
Server configuration utilities:
- `Config` - HTTP server configuration struct
- `DefaultConfig()` - Create server config with sensible defaults
- `CreateServer()` - Create HTTP server with configuration

## Usage

Import the shared library in your go.mod:

```go
require github.com/justbri/arrgo/shared v0.0.0

replace github.com/justbri/arrgo/shared => ../shared
```

Then use in your code:

```go
import (
    "github.com/justbri/arrgo/shared/config"
    "github.com/justbri/arrgo/shared/format"
    sharedhttp "github.com/justbri/arrgo/shared/http"
    "github.com/justbri/arrgo/shared/middleware"
    "github.com/justbri/arrgo/shared/server"
)
```
