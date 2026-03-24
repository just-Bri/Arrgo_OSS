# Plan: Dependency Injection Over Globals

## Status

### Completed

- **`MetadataService`** — TVDB/TMDB token caching, rate limiting, and all 17 metadata functions converted to methods. Global accessor via `SetGlobalMetadataService` / `GetGlobalMetadataService` as a stepping stone.
- **`SubtitleService`** — OpenSubtitles token caching, rate limiting, semaphore, and all subtitle download/sync functions converted to methods. Global accessor via `SetGlobalSubtitleService` / `GetGlobalSubtitleService`.
- **`AutomationService`** — was already struct-based before this effort.
- **`QBittorrentClient`** — was already struct-based before this effort.

### Remaining candidates (in priority order)

#### 1. Remove the global accessors for MetadataService and SubtitleService

The `SetGlobal*` / `GetGlobal*` pattern was a pragmatic stepping stone. The next step is to pass these service instances through constructors rather than accessing them via globals. This means:

- `AutomationService` should take `*MetadataService` and `*SubtitleService` in its constructor instead of accessing `globalMetadata` / `globalSubtitle`
- Handler functions that call `GetGlobalMetadataService()` / `GetGlobalSubtitleService()` should receive the service via handler structs (see Phase 2 below)
- `movies.go` and `shows.go` call `globalMetadata.MatchMovie/MatchShow` — these should accept a `*MetadataService` parameter or become methods on a service that holds a reference

#### 2. Handler structs (moderate value)

Handlers currently call `config.Load()` on every request and reach into global services. Converting to handler structs would:
- Eliminate repeated `config.Load()` calls
- Make dependencies explicit
- Move template parsing out of `init()` into constructors

```go
type MovieHandler struct {
    metadata  *services.MetadataService
    templates *template.Template
}
```

This is a large surface area change (every handler file) but mechanically simple. Best done one handler file at a time.

#### 3. MovieService / ShowService (lower value)

`movies.go` and `shows.go` each have one scan mutex as package-level state, and 2-3 functions that take `cfg`. The benefit of converting these to structs is marginal — you'd be adding a struct mostly to hold one mutex. Consider doing this only if:
- You add tests that need to control the DB connection
- The scan functions grow more shared state
- You want consistency with MetadataService/SubtitleService

#### 4. database.DB global (foundational but large)

The `database.DB` global is used directly in ~40+ places across handlers and services. Removing it means passing `*sql.DB` through every constructor. This is the "final boss" of the DI migration — high value for testability, but touches almost every file.

Recommended approach: do this last, after all services are struct-based, so `database.DB` is only used in `main.go` to construct the services.

### What to leave alone

- **`session.go`** — initialized once, application-wide, simple API. No benefit from a struct.
- **`scan_status.go`** — scan state is inherently application-wide (one scan per type). Clean API already.
- **`renamer.go`** — per-path mutex map is intentionally shared across all rename operations. The design is correct as-is.
- **`HasSubtitles()`** — pure filesystem function, no state. Stays package-level.
