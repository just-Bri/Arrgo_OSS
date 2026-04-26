# Arrgo 🏴‍☠️

Arrgo is a lightweight, high-performance media management tool designed as a modern, consolidated alternative to the existing *arr stack.

**Built for Unraid from the ground up**, Arrgo is optimized for home server environments that value speed, simplicity, and low resource overhead.

---

## 🚀 Key Features

- **Consolidated Management**: Handle both movies and TV shows in one unified interface — replaces Radarr, Sonarr, Bazarr, and Overseerr.
- **Automated Workflows**: Automated downloads via qBittorrent, intelligent seeding cleanup, and integrated subtitle fetching.
- **Media Server Compatibility**: Automatically organizes and renames files following [Jellyfin](https://jellyfin.org/docs/general/server/media/movies/) and [Plex](https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/) conventions, including writing `movie.nfo` / `tvshow.nfo` files so Jellyfin treats Arrgo as the matching authority.
- **Deep Metadata**: Powered by TMDB and TVDB for rich posters, descriptions, and episode-level library status.
- **Lightweight & Fast**: Built with Go and HTMX for minimal resource usage — perfect for the Unraid ecosystem.
- **Unraid First**: Native PUID/PGID support, simple path mapping, and a dedicated XML template for the Community Applications store.

---

## 🛠 Installation

### 1. Unraid (Recommended)
The easiest way to install Arrgo is via the **Unraid Community Applications (CA)** store.

Search for **Arrgo** in the "Apps" tab. The official image is hosted at `justbri/arrgo_oss`.

1. Configure the required environment variables (see [Configuration](#%EF%B8%8F-configuration-details) below).
2. Map your **Media** and **AppData** paths.

> [!NOTE]
> Arrgo requires a **PostgreSQL 16** database. If you don't have one, install the official Postgres image from the App Store.

### 2. Docker Compose
If you prefer managing stacks via **Dockge**, **Compose Manager**, or plain Docker Compose:

```bash
git clone https://github.com/just-Bri/Arrgo_OSS.git
cd Arrgo_OSS

cp .env.example .env
# Edit .env with your actual values

docker compose up -d
```

---

## ⚡ Configuration Details

### Required Environment Variables

| Variable | Description |
| :--- | :--- |
| `SESSION_SECRET` | Secure key for session management (any random string) |
| `DATABASE_URL` | PostgreSQL connection string, e.g. `postgres://user:pass@db:5432/arrgo?sslmode=disable` |
| `TMDB_API_KEY` | [TheMovieDB API Key](https://www.themoviedb.org/documentation/api) |
| `TVDB_API_KEY` | [TheTVDB API Key](https://thetvdb.com/api-information) |
| `ADMIN_PASSWORD` | Initial password for the seeded admin account |

### qBittorrent Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `QBITTORRENT_URL` | `http://qbittorrent:8080` | URL for qBittorrent WebUI |
| `QBITTORRENT_USER` | — | qBittorrent admin username |
| `QBITTORRENT_PASS` | — | qBittorrent admin password |

### Jellyfin Variables (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `JELLYFIN_URL` | `http://jellyfin:8096` | URL to your Jellyfin server |
| `JELLYFIN_API_KEY` | — | Jellyfin API key (Dashboard → API Keys) |

### Subtitle Variables (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `OPENSUBTITLES_API_KEY` | — | [OpenSubtitles](https://www.opensubtitles.com/) API key |
| `OPENSUBTITLES_USER` | — | OpenSubtitles username |
| `OPENSUBTITLES_PASS` | — | OpenSubtitles password |
| `ENABLE_SUBSYNC` | `false` | Set to `true` to enable automatic subtitle synchronization |
| `FFSUBSYNC_URL` | `http://ffsubsync-api:8080` | URL for the `ffsubsync-api` sidecar service |

### Other Optional Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `5003` | HTTP server port |
| `ADMIN_USERNAME` | `admin` | Username for the seeded admin account |
| `ADMIN_EMAIL` | `admin@arrgo.local` | Email for the seeded admin account |
| `PUID` / `PGID` | `99` / `100` | User/Group ID for file permissions (Unraid defaults) |
| `UMASK` | `002` | File creation umask |
| `MOVIES_PATH` | `/data/movies` | Path to movies library |
| `SHOWS_PATH` | `/data/shows` | Path to shows library |
| `INCOMING_MOVIES_PATH` | `/data/incoming/movies` | Staging path for incoming movies |
| `INCOMING_SHOWS_PATH` | `/data/incoming/shows` | Staging path for incoming shows |
| `CLOUDFLARE_BYPASS_URL` | `http://byparr:8191` | [Byparr](https://github.com/ThePhaseless/Byparr) URL for Cloudflare-protected indexers |
| `DEBUG` | `false` | Set to `true` for verbose logging |

---

## 🛡 VPN Configuration (PIA)

Arrgo's default stack includes a pre-configured qBittorrent VPN container (`binhex/arch-qbittorrentvpn`) optimized for Private Internet Access.

> [!IMPORTANT]
> The binhex image selects the **first `.ovpn` file it finds** in the openvpn directory — it does not use the `VPN_CONFIG` environment variable for file selection. You must keep **only one `.ovpn` file** in the directory: the endpoint you want to use.

1. **Download PIA OpenVPN configs**:
   ```bash
   wget https://www.privateinternetaccess.com/openvpn/openvpn.zip
   mkdir -p ./config/qbittorrent/openvpn
   unzip openvpn.zip -d ./config/qbittorrent/openvpn/
   ```
2. **Keep only one config**: Delete all `.ovpn` files except the endpoint you want (e.g. keep only `ca_montreal.ovpn`).
   ```bash
   cd ./config/qbittorrent/openvpn
   ls *.ovpn | grep -v ca_montreal | xargs rm
   ```
3. **Credentials**: Set `PIA_USER` and `PIA_PASSWORD` in your `.env`.

> [!TIP]
> Choose an endpoint that supports port forwarding for best download performance. Montreal (`ca_montreal.ovpn`), Toronto, and Vancouver all support it. Brazil and most non-North-American endpoints do not.

---

## 📺 Jellyfin Integration (Optional)

Arrgo integrates with [Jellyfin](https://jellyfin.org/) for automatic library refreshes, user management, and metadata authority.

1. **Get an API key**: In Jellyfin, go to **Dashboard → API Keys** and create a new key.
2. **Set environment variables**: Add `JELLYFIN_URL` and `JELLYFIN_API_KEY` to your `.env`. If either is missing, Jellyfin integration is silently disabled.
3. **Features**:
   - **Auto library refresh**: Jellyfin's library is automatically refreshed after imports, scans, and deduplication.
   - **NFO files**: Arrgo writes `movie.nfo` and `tvshow.nfo` files containing TMDB/TVDB IDs alongside your media, making Arrgo the matching authority so Jellyfin always identifies files correctly.
   - **User sync**: New Arrgo users automatically get a Jellyfin account created with a temporary password (`changeme-{username}`). Existing users can be bulk-synced from the admin panel.
   - **Manual controls**: The admin panel provides buttons to trigger a library refresh or sync all users on demand.

---

## 🎬 Subtitle Synchronization (Optional)

Arrgo includes an optional integration with `ffsubsync` to automatically sync subtitles with video files.

1. **Enable the feature**: Set `ENABLE_SUBSYNC=true` in your `.env`.
2. **Architecture**: Arrgo communicates with the `ffsubsync-api` sidecar container included in the default stack. By default it stays idle unless `ENABLE_SUBSYNC=true`.
3. **Usage**: Once enabled, Arrgo will offer options to automatically synchronize downloaded subtitles for perfect timing.

---

## 💡 Tips for Unraid Users

### Paths & Permissions
- **Media Shares**: Map your root media share (e.g. `/mnt/user/media`) to the container's `/data` path. This ensures atomic moves between `incoming/` and the library without copying data across shares.
- **Hardlinks**: Arrgo supports hardlinking when your downloads and media library share the same Unraid share and Docker volume mapping.

### Troubleshooting: Force Rebuild
Arrgo builds from source on first run. If you are developing on an SMB share and changes aren't reflecting, increment the `BUILD_VERSION` in your `docker-compose.yml` to force a fresh Docker layer build.

---

## 🗺 Roadmap

- [x] Basic Login & Authentication (bcrypt)
- [x] Unified Search (Local + TMDB/TVDB)
- [x] Library Scanner & Auto-renaming
- [x] Show/Season/Episode Support
- [x] Subtitle Management (OpenSubtitles)
- [x] qBittorrent Integration & Automation
- [x] Subtitle sync via ffsubsync microservice
- [x] Jellyfin integration (library refresh & user sync)
- [x] NFO file writing (Arrgo as Jellyfin matching authority)
- [ ] Jellyfin auto-collections
- [ ] Advanced Library Filtering & Bulk Actions

---

## ⚖️ License

Distributed under the **MIT License**. See `LICENSE` for more information.
