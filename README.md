# Arrgo

Arrgo is a lightweight, high-performance media management tool designed to replace traditional *arr stacks (Sonarr, Radarr, and eventually Bazarr). It's built for speed, simplicity, and ease of deployment.

## 🚀 Goals

The primary goal of Arrgo is to provide a modern, consolidated alternative to the existing media management ecosystem, specifically optimized for home server environments like Unraid.

- **Consolidated Management**: Handle both movies and shows in one application.
- **Media Server Compatibility**: Automatically organize and rename files to follow [Plex](https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/) and [Jellyfin](https://jellyfin.org/docs/general/server/media/movies/) naming conventions.
- **Lightweight & Fast**: Built with Go and HTMX to ensure minimal resource usage and a snappy user interface.
- **Docker-First**: Designed to be easily deployed via Docker Compose.
- **Simplicity**: Start simple with core functionality and expand over time.

## 🛠 Tech Stack

- **Backend**: [Go](https://go.dev/)
- **Frontend**: [HTMX](https://htmx.org/) (Server-side rendered Go templates)
- **Database**: [PostgreSQL](https://www.postgresql.org/) (Chosen for robust, relational long-term storage)
- **Metadata**: [TMDB](https://www.themoviedb.org/) (Movies) & [TVDB](https://www.thetvdb.com/) (TV Shows)
- **Deployment**: [Docker](https://www.docker.com/) & [Docker Compose](https://docs.docker.com/compose/)

## 🌟 Current Features

- **Consolidated Management**: Handle both movies and shows in one application.
- **Unified Search**: Search across your local library and external sources (TMDB/TVDB) simultaneously from any page.
- **Media Details**: Deep dive into your library with rich metadata, posters, and episode-level library status.
- **Request System**: Users can request new movies or specific seasons of shows.
- **Secure Auth**: Password-based login with bcrypt hashing and session management.
- **Role-Based Access**: Restrict dangerous actions (like library scans) to admin users.
- **Automatic Organization**: Automatically organize and rename files to follow standard naming conventions.
- **Smart Scanning**: Split scanning logic for library and incoming media, with a background worker that automatically polls for new media hourly.
- **Cross-Device Support**: Efficiently handles moving media across different disks or mount points with a safe fallback mechanism.
- **Library Sanitization**: Automatically cleans up database records for files or folders that have been deleted or moved manually.

## 📂 Project Structure

- `/handlers`: HTTP request handlers and routing logic.
- `/models`: Database schemas and data structures.
- `/services`: Core business logic and external integrations.
- `/templates`: HTML templates using Go's `html/template` engine with HTMX enhancements.
- `/static`: Static assets (CSS, JS, images).
- `/database`: Connection management, schema initialization, and seeding logic.
- `/config`: Application configuration handling.

## ⚙️ Getting Started

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### Deployment

To get started on your home server (like Unraid):

1. Clone the repository
2. Copy `.env.example` to `.env` and fill in your configuration values:
   ```bash
   cp .env.example .env
   # Edit .env with your actual values
   ```
3. Run Docker Compose:
   ```bash
   docker-compose up -d
   ```

The application will be available at `http://localhost:5003`.

### Environment Variables

Configure the following variables in your `.env` file (see `.env.example` for a complete list):

**Required:**
- `SESSION_SECRET`: Key used for secure session management (generate a random secret).
- `POSTGRES_PASSWORD`: Password for the PostgreSQL database.
- `TMDB_API_KEY`: Your [TheMovieDB API Key](https://www.themoviedb.org/documentation/api) (Required for metadata).
- `TVDB_API_KEY`: Your [TheTVDB API Key](https://thetvdb.com/api-information) (Required for TV shows).
- `OPENSUBTITLES_API_KEY`: Your [OpenSubtitles.com API Key](https://www.opensubtitles.com/en/consumers) (Required for subtitles).
- `OPENSUBTITLES_USER`: Your OpenSubtitles username.
- `OPENSUBTITLES_PASS`: Your OpenSubtitles password.
- `QBITTORRENT_USER`: qBittorrent WebUI username.
- `QBITTORRENT_PASS`: qBittorrent WebUI password.
- `ADMIN_PASSWORD`: Initial admin account password (used for seeding).

**Optional (with defaults):**
- `PORT`: The port the application will listen on (default: 5003).
- `PUID/PGID`: User/Group ID for file permissions (default: 99/100 for Unraid).
- `MOVIES_PATH`: Local path where your processed movies are stored (default: `/data/movies`).
- `SHOWS_PATH`: Local path where your processed shows are stored (default: `/data/shows`).
- `INCOMING_MOVIES_PATH`: Path where new, unprocessed movies are located (default: `/data/incoming/movies`).
- `INCOMING_SHOWS_PATH`: Path where new, unprocessed shows are located (default: `/data/incoming/shows`).
- `DEBUG`: Set to `true` for verbose logging (default: `false`).

**VPN Configuration (qBittorrent VPN container - PIA only):**
- `PIA_USER`: Private Internet Access username.
- `PIA_PASSWORD`: Private Internet Access password.
- `PIA_REMOTE`: Optional VPN server (leave empty for auto-select).

**Important Setup Steps:**
1. Download PIA OpenVPN configuration files:
   ```bash
   # Download the PIA OpenVPN configs
   wget https://www.privateinternetaccess.com/openvpn/openvpn.zip
   unzip openvpn.zip -d /mnt/user/appdata/dockge/stacks/arrgo/qbittorrent/config/openvpn/
   ```
2. Remove any non-PIA `.ovpn` files (like ProtonVPN) from `/mnt/user/appdata/dockge/stacks/arrgo/qbittorrent/config/openvpn/`
3. Ensure your `.env` file has `PIA_USER` and `PIA_PASSWORD` set correctly
4. The container will use the PIA `.ovpn` files with your credentials to connect

### 💡 Tips for Unraid Users

Arrgo is specifically optimized for Unraid. When deploying:

1. **Volume Mappings**: Ensure your media paths in `docker-compose.yml` point to your `/mnt/user/...` shares.
2. **Permissions**: Use `PUID=99` and `PGID=100` (default Unraid user) to ensure the application has correct access to your media files.
3. **Database**: The database is mapped to `./db_data` by default, ensuring your metadata and settings persist in your appdata folder.

### 🛠 Troubleshooting: Forcing a Rebuild

If Dockge or Unraid is "stuck" using an old version of the code (common with SMB shares), you can force a fresh build without leaving the UI:

1. Click **Edit** on the stack in Dockge.
2. In the `docker-compose.yml` section, look for `BUILD_VERSION: 1`.
3. Increment the number (e.g., change `1` to `2`).
4. Click **Deploy**.

This invalidates the Docker build cache and forces it to pick up your latest file changes.

## 🗺 Roadmap

- [x] Basic Login & Authentication (bcrypt hashing)
- [x] User Registration & Admin Permissions
- [x] Dashboard with Movie/Show Library Overview
- [x] Unified Search (Local Library + TMDB/TVDB)
- [x] Media Details Pages (Extended metadata, episode lists)
- [x] Library Scanner (Automatic detection of new media)
- [x] Movie Metadata & Organization (TMDB integration, Auto-renaming)
- [x] Unraid Optimization (Permissions, Docker-ready)
- [x] Show Organization (Auto-renaming episodes)
- [ ] Subtitle Management (Bazarr functionality)
- [ ] Integration with Download Clients (qBittorrent, etc.)
- [ ] User Management UI (Promote/Demote admins)
- [ ] Advanced Library Filtering & Bulk Actions

## 📝 Note on Database Choice

While Redis was considered for its speed, **PostgreSQL** was chosen for the primary data store and metadata caching. Media management involves complex relational data (shows -> seasons -> episodes -> files) and heavy metadata (summaries, cast, etc.), making a relational database with JSONB support the superior choice for long-term robustness and resource efficiency on home servers.
