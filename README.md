# Arrgo 🏴‍☠️

Arrgo is a lightweight, high-performance media management tool designed to provide a modern, consolidated alternative to the existing *arr stack.  
Built for speed and simplicity, it is specifically optimized for home server environments like **Unraid**.

## 🚀 Key Features & Goals

- **Consolidated Management**: Handle both movies and tv shows in one unified interface.
- **Automated Workflows**: Automated downloads via qBittorrent, intelligent seeding cleanup, and integrated subtitle fetching.
- **Media Server Compatibility**: Automatically organizes and renames files to follow [Plex](https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/) and [Jellyfin](https://jellyfin.org/docs/general/server/media/movies/) conventions.
- **Deep Metadata**: Powered by TMDB and TVDB for rich posters, descriptions, and episode-level library status.
- **Lightweight & Fast**: Built with Go and HTMX for minimal resource usage and a snappy UI.
- **Home Server First**: Designed with Unraid in mind, featuring native PUID/PGID support and simple Docker deployment.

## 🛠 Tech Stack

- **Backend**: [Go](https://go.dev/)
- **Frontend**: [HTMX](https://htmx.org/)
- **Database**: [PostgreSQL 16](https://www.postgresql.org/)
- **Infrastructure**: [Docker](https://www.docker.com/) & [Docker Compose](https://docs.docker.com/compose/)

## ⚡ Quick Start

### 1. Deployment

```bash
# Clone the repository
git clone https://github.com/justbri/Arrgo.git
cd Arrgo

# Configure environment
cp .env.example .env
# Edit .env with your actual values

# Start the stack
docker-compose up -d
```

The application will be available at `http://localhost:5003`.

### 2. Required Environment Variables

Ensure these are set in your `.env` file for core functionality:

| Variable | Description |
| :--- | :--- |
| `SESSION_SECRET` | Secure key for session management (generate a random string) |
| `POSTGRES_PASSWORD` | Password for the PostgreSQL container |
| `TMDB_API_KEY` | [TheMovieDB API Key](https://www.themoviedb.org/documentation/api) |
| `TVDB_API_KEY` | [TheTVDB API Key](https://thetvdb.com/api-information) |
| `ADMIN_PASSWORD` | Initial password for the seeded admin account |

### 3. Optional Configuration

| Variable | Default | Description |
| :--- | :--- | :--- |
| `SERVER_IP` | `localhost` | IP address of your server (e.g. 192.168.1.100) |
| `MEDIA_PATH` | `/mnt/user/media` | **Host** path to your media library |
| `MOVIES_PATH` | `/data/movies` | **Container** path for processed movies |
| `SHOWS_PATH` | `/data/shows` | **Container** path for processed shows |
| `PUID/PGID` | `99`/`100` | User/Group ID for file permissions (Unraid defaults) |
| `QBITTORRENT_URL` | `http://qbittorrent:8080` | URL for qBittorrent WebUI |
| `DEBUG` | `false` | Set to `true` for verbose logging |

---

## 🛡 VPN Configuration (PIA Only)

Arrgo includes a pre-configured qBittorrent VPN stack (based on `binhex/arch-qbittorrentvpn`) optimized for Private Internet Access.

1. **Download PIA OpenVPN configs**:
   ```bash
   wget https://www.privateinternetaccess.com/openvpn/openvpn.zip
   # Create directory if it doesn't exist
   mkdir -p ./config/qbittorrent/openvpn
   unzip openvpn.zip -d ./config/qbittorrent/openvpn/
   ```
2. **Clean up**: Remove any non-PIA `.ovpn` files from the directory.
3. **Credentials**: Set `PIA_USER` and `PIA_PASSWORD` in your `.env`.

---

## 💡 Tips for Unraid Users

### Deployment Method
Unraid does not support Docker Compose natively in the "Docker" tab. To deploy Arrgo, you should use:
- **[Dockge](https://github.com/louislam/dockge)** (Highly Recommended): A beautiful, easy-to-use self-hosted manager for Docker Compose stacks.
- **Docker Compose Manager Plugin**: Available via the Unraid Community Applications (CA) store.

### Configuration
- **Network**: Set `SERVER_IP` in `.env` to your Unraid server's IP (e.g., `192.168.1.100`) so the qBittorrent button works correctly.
- **Media**: Set `MEDIA_PATH` in `.env` to your share (e.g., `/mnt/user/media`).
- **Database**: The database is mapped to `./data/db` within the project folder. You can add this path to your backup solution.

## 🛠 Troubleshooting: Force Rebuild

If your deployment is stuck using old code (common with SMB shares), click **Edit** on your stack in Dockge, increment the `BUILD_VERSION` number in the `docker-compose.yml` section, and click **Deploy**. This forces a fresh build by invalidating the Docker cache.

## 🗺 Roadmap

- [x] Basic Login & Authentication (bcrypt)
- [x] Unified Search (Local + TMDB/TVDB)
- [x] Library Scanner & Auto-renaming
- [x] Show/Season/Episode Support
- [x] Subtitle Management (OpenSubtitles)
- [x] qBittorrent Integration & Automation
- [ ] Advanced Library Filtering & Bulk Actions

## ⚖️ License

Distributed under the **MIT License**. See `LICENSE` for more information.
