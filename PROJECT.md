# Arrgo — Project Breakdown

A living technical reference covering architecture, design decisions, and implementation details. Complements the user-facing `README.md`.

---

## What It Is

Arrgo is a self-hosted media management application — a consolidated alternative to the *arr stack (Radarr, Sonarr, etc.). It handles movies and TV shows in a single app: library scanning, TMDB/TVDB metadata, automated torrent downloads via qBittorrent, file renaming to Plex/Jellyfin naming conventions, subtitle fetching and synchronization, and optional Jellyfin integration.

Built for Unraid, deployed via Docker. Primary users are home server operators who want one app instead of many.

---

## Repository Structure

Multi-module Go monorepo. Each directory with a `go.mod` is an independent module with its own build.

```
Arrgo/
├── server/          # Main application (Go + HTMX frontend)
├── indexer/         # Torznab-compatible indexer service
├── ffsubsync-api/   # Subtitle synchronization microservice (Go + Python)
├── shared/          # Shared libraries consumed by other modules
├── docs/            # Planning docs, OpenAPI specs
├── Dockerfile       # Main app image (multi-stage Go → Alpine)
├── docker-compose.yml
└── entrypoint.sh
```

Each module uses `replace` directives in its `go.mod` to reference `shared/` locally, so changes to shared propagate without publishing to a registry.

---

## Modules

### `server/` — Main Application

The core of Arrgo. Serves the web UI, manages the database, runs background workers.

**Internal packages:**

| Package | Purpose |
|---------|---------|
| `config/` | Loads all configuration from environment variables |
| `database/` | PostgreSQL connection pool, migration runner, seeder |
| `handlers/` | HTTP route handlers — thin layer, delegates to services |
| `middleware/` | Auth middleware (`RequireAuth`) |
| `models/` | Shared data structs (Movie, Show, Request, User, etc.) |
| `services/` | All business logic — the heaviest package |
| `templates/` | Go HTML templates rendered server-side |
| `static/` | Favicon and app icons |

**Entry point:** `server/main.go`
- Loads config, initializes logger, session store, database
- Runs migrations, seeds admin user
- Instantiates all services (metadata, subtitle, automation)
- Starts background workers (incoming scanner, cleanup worker, automation loop)
- Wires dependency injection via `handlers.NewHandlers()`
- Registers all routes via `setupRoutes()`
- Starts HTTP server with graceful shutdown on SIGTERM/SIGINT

**Port:** 5003

---

### `indexer/` — Torznab Indexer

A standalone HTTP service that exposes a [Torznab](https://torznab.github.io/spec-1.3-draft/) API. Torznab is the XML-based search protocol that `*arr` apps use to talk to indexers — this allows Arrgo's automation to query torrent sites in a standardized way.

**Handlers:** `indexer/handlers/torznab.go`
**Port:** 5004

---

### `ffsubsync-api/` — Subtitle Sync Service

Wraps the Python [`ffsubsync`](https://github.com/smacke/ffsubsync) CLI in a Go HTTP API. Takes a video file path and a subtitle file path, runs ffsubsync, returns the synchronized subtitle.

Only active when `ENABLE_SUBSYNC=true`. The Dockerfile installs ffmpeg, Python 3, and ffsubsync in a Debian Bookworm image.

**Port:** 8080

---

### `shared/` — Shared Libraries

Consumed via `replace` directives. Contains no domain logic — only utilities.

| Sub-package | Contents |
|-------------|----------|
| `config/` | `GetEnv`, `GetEnvRequired` helpers |
| `format/` | Human-readable bytes, string utilities |
| `http/` | Default HTTP clients, FlareSolverr bypass support |
| `logger/` | Structured logging (`slog`) initialization |
| `middleware/` | Request logging middleware |
| `server/` | HTTP server config helpers, `CreateServer` |
| `indexers/` | Torrent site scrapers: 1337x, Nyaa, YTS, TorrentGalaxy, SolidTorrents |

---

## Database

**PostgreSQL 16.** Schema managed via sequential SQL migration files in `server/database/migrations/`.

**Core tables:**

| Table | Description |
|-------|-------------|
| `users` | Accounts with bcrypt password hashes, is_admin flag |
| `movies` | Library entries with TMDB metadata, file path, quality, torrent hash |
| `shows` | TV series with TVDB/TMDB metadata |
| `seasons` | Season containers, child of shows |
| `episodes` | Episode files with path, quality, torrent hash |
| `requests` | User-submitted media requests (movies or shows) with retry state |
| `downloads` | Active qBittorrent downloads linked to requests |
| `subtitle_queue` | Queue for async subtitle fetching with retry backoff |
| `indexers` | Registry of configured torrent indexers |
| `tvdb_episodes` | Cached TVDB episode data |
| `settings` | Key/value app settings |

**Cascade relationships:** episodes → seasons → shows, downloads → requests → users

---

## Services Layer

The `services/` package is where most complexity lives. Key services:

**`MetadataService`** (`metadata.go`, ~38KB)
- Queries TMDB and TVDB APIs
- Fetches movie/show/episode metadata, posters
- Caches TVDB episode data in `tvdb_episodes` table

**`AutomationService`** (`automation.go`, ~100KB)
- Polls qBittorrent for download progress
- When a torrent completes: triggers import → rename → subtitle fetch
- Handles retry logic for failed requests
- Manages the full request lifecycle (searching → downloading → importing)

**`SubtitleService`** (`subtitles.go`, ~32KB)
- Queries OpenSubtitles API for matching subtitles
- Optionally sends subtitle + video to ffsubsync-api for sync
- Manages `subtitle_queue` with retry backoff

**`QBittorrentClient`** (`qbittorrent.go`)
- HTTP client for qBittorrent WebUI API
- Handles login, torrent add/remove/status

**Other services:**
- `movies.go`, `shows.go` — Library management, import logic
- `renamer.go` — Plex/Jellyfin-compatible file naming (~32KB)
- `scanner_worker.go` — Library directory scanner
- `jellyfin.go` — Jellyfin API integration (library refresh, user sync)
- `video_inspector.go` — ffprobe wrapper for quality detection
- `seeding_cleanup.go` — Removes torrents after seeding ratio/time met

### Dependency Injection

Services are instantiated in `main.go` and injected into `handlers.Handlers` via `handlers.NewHandlers()`. Handlers that need service access hold them via the `*Handlers` struct rather than global variables.

Some internal cross-service calls (within the `services` package) use unexported package-level vars (`globalMetadata`, `globalSubtitle`) set by `NewAutomationService()` — this is intentional and scoped entirely within the package.

---

## HTTP Layer

**Router:** [`go-chi/chi`](https://github.com/go-chi/chi) v5

**Middleware stack (global):**
- `RequestID`, `RealIP`, `CleanPath` — chi standard
- `sharedmiddleware.Logging` — structured request logs
- `Recoverer` — panic recovery
- `Timeout(60s)` — request timeout
- `Compress(5)` — gzip compression

**Auth:** Session-based via cookie. `RequireAuth` middleware checks the session store. Login/logout/register are public routes; everything else requires auth.

**Route groups:**
- Public: `/ping`, `/login`, `/register`, `/logout`
- Protected (requires auth): all UI pages, all API endpoints, all scan/admin actions
- Static/media: `/static/*`, `/images/tmdb/*`, `/images/movie/*`, `/images/shows/*`

---

## Frontend

**Technology:** Server-rendered Go HTML templates + [htmx](https://htmx.org/) 2.0.7 + [missing.css](https://missing.style/) 1.2.0

**Philosophy:** Semantic HTML first. missing.css is a classless framework — it styles standard HTML elements (`<article>`, `<fieldset>`, `<table>`, `<form>`, `<label>`, etc.) without requiring classes. This keeps templates clean and reduces custom CSS to a minimum.

**Dark mode only.** `<html data-theme="dark">` is hardcoded. No light mode, no theme toggle.

**Template structure:**

```
templates/
├── layouts/
│   └── base.html        # <html>, <head>, global CSS vars, utility classes
├── pages/
│   ├── dashboard.html
│   ├── login.html
│   ├── register.html
│   ├── movies.html
│   ├── shows.html
│   ├── movie_details.html
│   ├── show_details.html
│   ├── requests.html
│   ├── search.html
│   ├── admin.html
│   └── settings.html
└── components/
    ├── navigation.html
    ├── admin_user_info.html
    ├── admin_library_management.html
    ├── admin_library_maintenance.html
    ├── admin_jellyfin.html
    ├── admin_subtitle_management.html
    ├── admin_incoming_media.html
    └── admin_danger_zone.html
```

**CSS approach:**
- missing.css CDN handles typography, forms, tables, cards (`<article>`), grouping (`<fieldset>`/`<legend>`)
- `base.html` defines CSS custom properties (`--accent-color`, `--border-color`, etc.) and utility classes (`.poster-card`, `.details-card`, `.media-row`, `.info-grid`, `.modal-overlay`, `.spinner`)
- Buttons: plain `<button>` everywhere; only the "Nuke Database" button has `class="bad"` (missing.css danger color)
- Admin sections use `<fieldset>`/`<legend>` for zero-CSS visual separation

**htmx usage:** Partial page updates for scan status polling, subtitle downloads, alternatives modals, and request actions. Avoids full page reloads for interactive operations.

---

## Background Workers

All workers start in `main.go` and run as goroutines:

| Worker | Trigger | Purpose |
|--------|---------|---------|
| `AutomationService.Start()` | Continuous loop | Polls qBittorrent, processes completed downloads, retries failed requests |
| `StartIncomingScanner()` | Timer | Watches incoming directories for new files |
| `StartCompletedRequestsCleanupWorker()` | Timer | Removes old completed/denied requests |
| `StartSeedingCleanupWorker()` | Timer | Removes torrents that have hit seeding goals |

All workers respect a shared `context.Context` cancelled on shutdown for clean termination.

---

## Docker / Deployment

**Stack management:** Dockge (or Docker Compose Manager on Unraid). The stack lives in one `docker-compose.yml`.

**Services in the stack:**

| Service | Image | Port | Purpose |
|---------|-------|------|---------|
| `arrgo` | Built from `./Dockerfile` | 5003 | Main application |
| `db` | `postgres:16-alpine` | 5433 | Database |
| `qbittorrent` | `binhex/arch-qbittorrentvpn` | 8080 | Torrent client + PIA VPN |
| `byparr` | FlareSolverr | 8191 | Cloudflare bypass for indexers |
| `ffsubsync-api` | Built from `./ffsubsync-api/Dockerfile` | 8080 | Subtitle sync (optional) |

**Network:** External Docker network `coven` — all services communicate by service name.

**Permissions:** `entrypoint.sh` creates a user matching `PUID`/`PGID` (default `99:100` for Unraid), chowns only writable data directories (`/app/data`, `/app/logs`, `/app/cache`), then drops to that user via `su-exec` before running the binary.

**Rebuilding a single service in Dockge:** Edit `docker-compose.yml` (e.g., bump a `BUILD_VERSION` arg), then use Dockge's per-service rebuild button, or run `docker compose up -d --build arrgo` from the stack directory.

---

## Key Design Decisions

**Why Go + HTMX instead of a JS framework?**
Low resource usage is a core goal. Go compiles to a single binary; HTMX adds interactivity without a JS build step or runtime. The entire server binary + templates deploys as a single Alpine container under 50MB.

**Why missing.css?**
Classless/semantic approach means templates stay readable and close to plain HTML. The framework does the heavy lifting for dark mode, form styling, and typography. Custom CSS is minimal by design.

**Why PostgreSQL instead of SQLite?**
Multi-user support and concurrent write safety. Unraid users typically already run a Postgres container.

**Why a separate ffsubsync microservice?**
ffsubsync requires Python and ffmpeg — pulling those into the main Go Alpine image would bloat it significantly. Isolating it in a separate optional service keeps the main image lean and the feature truly optional.

**Why a separate indexer service?**
Clean separation of concerns. The indexer exposes a standard Torznab API, which means other apps (Radarr, etc.) could theoretically query it too. It also lets the indexer be scaled or replaced independently.

---

## Development Notes

- **Tool versions managed via [mise](https://mise.jdx.dev/)** (`mise.toml` at root)
- **Cannot be built/tested locally** — media paths and external services (qBittorrent, Jellyfin, TMDB) only exist on the home server. Development cycle: push changes → rebuild Docker image on Unraid/Dockge
- **Migrations run automatically** on startup via `database.RunMigrations()` — add new `.sql` files to `server/database/migrations/` with the next sequence number
- **Admin user seeded** on first startup from `ADMIN_USERNAME`/`ADMIN_PASSWORD` env vars
- **Plans directory** (`docs/plans/`) contains design documents for future features: targeted library scans, Jellyfin collections, missing episode detection, testing strategy
