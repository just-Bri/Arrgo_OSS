What Arrgo Is

**Arrgo** is a self-hosted, all-in-one media management app — a consolidated alternative to Radarr + Sonarr + Prowlarr. It manages movies and TV shows from a single unified Go web application, with HTMX frontend, PostgreSQL, native qBittorrent integration, optional Jellyfin sync, and subtitle auto-download/sync.

It's built **Unraid-first**: PUID/PGID, simple path mapping, and a Community Applications XML template.

---

## The 4 Services

| Service | Language | Port | Role |
|---|---|---|---|
| `arrgo` | Go | 5003 | Main web app — UI, automation, file management |
| `db` | PostgreSQL 16 | 5433 | All persistent data |
| `qbittorrent` | Docker | 8080 | Torrent client with PIA VPN |
| `ffsubsync-api` | Go + Python | 8080 (internal) | Subtitle timing sync micro-service |

Plus optional externals: FlareSolverr (Cloudflare bypass for 1337x), Jellyfin (media server).

---

## Issues Found (All Confirmed Against Real Code)

### 🔴 High

1. **Double logging middleware** — `sharedmiddleware.Logging` is registered on the chi router *and* wrapped around the mux again in `sharedserver.CreateServer(srvConfig, sharedmiddleware.Logging(mux))`. Every request logs twice. Fix: pass `mux` directly.

2. **`indexer` imports `server` just for `services/indexers/`** — `indexer/go.mod` has `replace Arrgo => ../server`, pulling in all of the server's deps (pgx, gorilla sessions) and requiring Dockerfile `sed` patching at build time. Fix: move `services/indexers/` into `shared/`.

3. **Go `1.26.1` doesn't exist** — All `go.mod` files, `mise.toml`, and the Dockerfile all reference a non-existent Go version. Fix: pin to `1.24.3` or the latest actual stable release.

4. **`ADMIN_PASSWORD` defaults to `"admin"`** — `seed.go` falls back to `"admin"` and `config.go` never validates it's set. Fix: require it in config validation.

### 🟡 Medium

5. **`LoggingSimple` is identical to `Logging` and unused** in `shared/middleware/logging.go`. Delete it.

6. **Deprecated shim files** (`http_client.go`, `utils.go`) exist in both `server/services/indexers/` and `indexer/providers/`, explicitly marked deprecated. Delete them.

7. **`ApproveRequestHandler` / `DenyRequestHandler` exist but aren't routed** in `main.go` — dead code. Wire them up or delete them.

8. **`"justbri"` hardcoded in Jellyfin sync exclusion** (`services/jellyfin.go`) — a developer username that leaked into production. Remove it; only `ADMIN_USERNAME` should be excluded.

9. **`ffsubsync-api` with `restart: unless-stopped` exits 0 when `ENABLE_SUBSYNC=false`** — Docker will keep restarting it. Fix: use `restart: on-failure`.

10. **`.env.example` doesn't exist** but README tells users to `cp .env.example .env`. Add it.

### 🔵 Low

11. Mixed `log`/`slog` usage (tracked in `docs/plans/code-hygiene.md`)
12. Global service singletons to phase out (tracked in `docs/plans/dependency-injection.md`)
13. Zero test files (plan in `docs/plans/testing-strategy.md`)
14. `server/tmp_arrgo`, `server/tmp_verify` compiled binaries not in `.gitignore`
