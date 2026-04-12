# Shared Library Summary

## Packages

### `shared/http`
HTTP client utilities used by both the server and indexer.
- `DefaultClient` — 15s timeout
- `LongTimeoutClient` — 30s timeout
- `MakeRequest()` — GET with context
- `FetchViaBypass()` — GET via Cloudflare bypass proxy
- `BuildQueryURL()` — URL with query params
- `DecodeJSONResponse()` — JSON decode helper

### `shared/format`
- `Bytes()` — Human-readable byte sizes (B, KB, MB, GB, TB…)
- `Preview()` — Truncated string preview for debug logging

### `shared/config`
- `GetEnv()` — Env var with default
- `GetEnvRequired()` — Required env var (panics if missing)

### `shared/middleware`
- `Logging` — Chi-compatible HTTP request logging middleware (skips `/api/scan/status` noise)

### `shared/server`
- `DefaultConfig()` — Sensible timeout defaults (Read: 15s, Write: 15s, Idle: 60s)
- `CreateServer()` — Creates `http.Server` from config

### `shared/logger`
- `Init()` — Initializes `log/slog` with the correct level based on `GOLOG_LOG_LEVEL` env var

### `shared/indexers`
Canonical torrent indexer implementations shared by both the server and indexer services.
- `SearchResult` — Common result type
- `Indexer` — Interface (`SearchMovies`, `SearchShows`, `Name`)
- `Indexers()` — Factory returning all enabled indexers
- Implementations: YTS, Nyaa (with 24h cache + `CleanupNyaaCache()`), 1337x, TorrentGalaxy, SolidTorrents

## Library Structure

```
shared/
├── go.mod
├── README.md
├── MIGRATION.md
├── SUMMARY.md
├── config/
│   └── env.go
├── format/
│   ├── bytes.go
│   └── preview.go
├── http/
│   └── client.go
├── indexers/
│   ├── indexer.go      # SearchResult, Indexer interface, Indexers() factory
│   ├── 1337x.go        # also defines shared parseSize() / extractQualityInfo()
│   ├── nyaa.go
│   ├── solid.go
│   ├── torrentgalaxy.go
│   └── yts.go
├── logger/
│   └── logger.go
├── middleware/
│   └── logging.go
└── server/
    └── config.go
```
