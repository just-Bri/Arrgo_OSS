# Arrgo 🏴‍☠️

Arrgo is a lightweight, high-performance media management tool designed to provide a modern, consolidated alternative to the existing *arr stack.  

**Built for Unraid from the ground up**, Arrgo is optimized for home server environments that value speed, simplicity, and low resource overhead.

---

## 🚀 Key Features

- **Consolidated Management**: Handle both movies and TV shows in one unified interface.
- **Automated Workflows**: Automated downloads via qBittorrent, intelligent seeding cleanup, and integrated subtitle fetching.
- **Media Server Compatibility**: Automatically organizes and renames files to follow [Plex](https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/) and [Jellyfin](https://jellyfin.org/docs/general/server/media/movies/) conventions.
- **Deep Metadata**: Powered by TMDB and TVDB for rich posters, descriptions, and episode-level library status.
- **Lightweight & Fast**: Built with Go and HTMX for minimal resource usage—perfect for the Unraid ecosystem.
- **Unraid First**: Native PUID/PGID support, simple path mapping, and a dedicated XML template for the Community Applications store.

---

## 🛠 Installation

### 1. Unraid (Recommended)
The easiest way to install Arrgo is via the **Unraid Community Applications (CA)** store.

Search for **Arrgo** in the "Apps" tab. The official image is hosted at `justbri/arrgo_oss`.

1. Configure the required environment variables:
   - `TMDB_API_KEY`: [TheMovieDB API Key](https://www.themoviedb.org/documentation/api)
   - `TVDB_API_KEY`: [TheTVDB API Key](https://thetvdb.com/api-information)
   - `DATABASE_URL`: Connection string for a PostgreSQL 16 database (e.g., `postgres://user:pass@192.168.1.100:5432/arrgo`)
   - `SESSION_SECRET`: A random string for session locking.
   - `ADMIN_PASSWORD`: Your initial login password.
2. Map your **Media** and **AppData** paths.

> [!NOTE]  
> Arrgo requires a **PostgreSQL 16** database. If you don't have one, we recommend the Official Postgres image from the App Store.

### 2. Dockge / Docker Compose (Advanced)
If you prefer managing stacks via **Dockge** or the **Docker Compose Manager** plugin:

```bash
# Clone the repository
git clone https://github.com/justbri/Arrgo_OSS.git
cd Arrgo_OSS

# Configure environment
cp .env.example .env
# Edit .env with your actual values

# Start the stack
docker-compose up -d
```

---

## ⚡ Configuration Details

### Required Environment Variables

| Variable | Description |
| :--- | :--- |
| `SESSION_SECRET` | Secure key for session management (generate a random string) |
| `DATABASE_URL` | PostgreSQL connection string |
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
| `JELLYFIN_API_KEY` | — | Jellyfin API key (from Jellyfin's Dashboard → API Keys) |

### Subtitle Variables (Optional)

| Variable | Default | Description |
| :--- | :--- | :--- |
| `OPENSUBTITLES_API_KEY` | — | [OpenSubtitles](https://www.opensubtitles.com/) API key |
| `OPENSUBTITLES_USER` | — | OpenSubtitles username |
| `OPENSUBTITLES_PASS` | — | OpenSubtitles password |
| `ENABLE_SUBSYNC` | `false` | Set to `true` to enable automatic subtitle synchronization |
| `FFSUBSYNC_URL` | `http://ffsubsync-api:8080` | URL for the `ffsubsync-api` service |

### Other Optional Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `PORT` | `5003` | HTTP server port |
| `TZ` | `America/Los_Angeles` | Container timezone |
| `SERVER_IP` | `localhost` | IP of your Unraid server (for qBittorrent links) |
| `PUID/PGID` | `99`/`100` | User/Group ID for file permissions (Unraid defaults) |
| `MOVIES_PATH` | `/data/movies` | Path to movies library |
| `SHOWS_PATH` | `/data/shows` | Path to shows library |
| `INCOMING_MOVIES_PATH` | `/data/incoming/movies` | Staging path for incoming movies |
| `INCOMING_SHOWS_PATH` | `/data/incoming/shows` | Staging path for incoming shows |
| `CLOUDFLARE_BYPASS_URL` | `http://host.docker.internal:8191` | [FlareSolverr](https://github.com/FlareSolverr/FlareSolverr) URL for Cloudflare-protected indexers |
| `DEBUG` | `false` | Set to `true` for verbose logging |

---

## 🛡 VPN Configuration (PIA Only)

Arrgo's default stack includes a pre-configured qBittorrent VPN container (based on `binhex/arch-qbittorrentvpn`) optimized for Private Internet Access.

1. **Download PIA OpenVPN configs**:
   ```bash
   wget https://www.privateinternetaccess.com/openvpn/openvpn.zip
   mkdir -p ./config/qbittorrent/openvpn
   unzip openvpn.zip -d ./config/qbittorrent/openvpn/
   ```
2. **Clean up**: Remove any non-PIA `.ovpn` files.
3. **Credentials**: Set `PIA_USER` and `PIA_PASSWORD` in your `.env`.

---

## 📺 Jellyfin Integration (Optional)

Arrgo can integrate with your [Jellyfin](https://jellyfin.org/) media server for automatic library refreshes and user management.

1. **Get an API key**: In Jellyfin, go to **Dashboard → API Keys** and create a new key.
2. **Set environment variables**: Add `JELLYFIN_URL` and `JELLYFIN_API_KEY` to your `.env`. If either is missing, Jellyfin integration is silently disabled.
3. **Features**:
   - **Auto library refresh**: Jellyfin's library is automatically refreshed after imports, scans, and deduplication operations.
   - **User sync**: New Arrgo users automatically get a Jellyfin account created with a temporary password (`changeme-{username}`). Existing users can be bulk-synced from the admin panel.
   - **Manual controls**: The admin panel provides buttons to trigger a library refresh or sync all users on demand.

---

## 🎬 Subtitle Synchronization (Optional)

Arrgo includes an optional integration with `ffsubsync` to automatically sync subtitles with video files.

1. **Enable the feature**: Set `ENABLE_SUBSYNC=true` in your `.env`.
2. **Architecture**: Arrgo communicates with the `ffsubsync-api` container in the stack. By default, this service is included but remains idle unless `ENABLE_SUBSYNC` is enabled.
3. **Usage**: Once enabled, Arrgo will provide options to automatically synchronize downloaded subtitles to ensure perfect timing.

---

## 💡 Tips for Unraid Users

### Paths & Permissions
- **Media Shares**: Always map your root media share (e.g., `/mnt/user/media`) to the container's `/data` path for optimal performance and atomic moves.
- **Hardlinks**: Arrgo supports hardlinking if your downloads and media library share the same Unraid share and Docker volume mapping.

### Troubleshooting: Force Rebuild
If you are developing or testing updates on an SMB share and changes aren't reflecting, increment the `BUILD_VERSION` in your `docker-compose.yml` (or App configuration) to force a fresh Docker layer build.

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
- [ ] Jellyfin auto-collections
- [ ] Advanced Library Filtering & Bulk Actions

---

## ⚖️ License

Distributed under the **MIT License**. See `LICENSE` for more information.
