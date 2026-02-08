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

### Optional Variables

| Variable | Default | Description |
| :--- | :--- | :--- |
| `SERVER_IP` | `localhost` | IP of your Unraid server (for qBittorrent links) |
| `PUID/PGID` | `99`/`100` | User/Group ID for file permissions (Unraid defaults) |
| `QBITTORRENT_URL` | `http://qbittorrent:8080` | URL for qBittorrent WebUI |
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
- [ ] Advanced Library Filtering & Bulk Actions

---

## ⚖️ License

Distributed under the **MIT License**. See `LICENSE` for more information.
