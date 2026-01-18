# Arrgo

Arrgo is a lightweight, high-performance media management tool designed to replace traditional *arr stacks (Sonarr, Radarr, and eventually Bazarr). It's built for speed, simplicity, and ease of deployment.

## 🚀 Goals

The primary goal of Arrgo is to provide a modern, consolidated alternative to the existing media management ecosystem, specifically optimized for home server environments like Unraid.

- **Consolidated Management**: Handle both movies (Radarr) and TV shows (Sonarr) in one application.
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

## 📂 Project Structure

- `/handlers`: HTTP request handlers and routing logic.
- `/models`: Database schemas and data structures.
- `/services`: Core business logic and external integrations.
- `/templates`: HTML templates using Go's `html/template` engine with HTMX enhancements.
- `/static`: Static assets (CSS, JS, images).
- `/database`: Connection management, migrations, and seeding logic.
- `/config`: Application configuration handling.

## ⚙️ Getting Started

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/)
- [Docker Compose](https://docs.docker.com/compose/install/)

### Deployment

To get started on your home server (like Unraid), clone the repository and run:

```bash
docker-compose up -d
```

The application will be available at `http://localhost:8080`.

### Environment Variables

Configure the following variables in `docker-compose.yml`:

- `DATABASE_URL`: Connection string for PostgreSQL.
- `SESSION_SECRET`: Key used for secure session management.
- `MOVIES_PATH`: Local path to your movie library.
- `PORT`: The port the application will listen on.

## 🗺 Roadmap

- [x] Basic Login & Authentication
- [x] Dashboard with Movie Library Overview
- [ ] Library Scanner & File Renaming (Plex/Jellyfin compatible)
- [ ] TV Show Management (Sonarr functionality)
- [ ] Metadata Scraping (Movie/Show details, cover art)
- [ ] Subtitle Management (Bazarr functionality)
- [ ] Integration with Download Clients (qBittorrent, etc.)
- [ ] Advanced Library Search and Filtering

## 📝 Note on Database Choice

While Redis was considered for its speed, **PostgreSQL** was chosen for the primary data store and metadata caching. Media management involves complex relational data (shows -> seasons -> episodes -> files) and heavy metadata (summaries, cast, etc.), making a relational database with JSONB support the superior choice for long-term robustness and resource efficiency on home servers.
