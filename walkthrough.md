# Arrgo Architectural Walkthrough

This document provides a high-level overview of the Arrgo project's architecture and functionality, based on an analysis of its components.

## What is Arrgo?

Arrgo is a lightweight, high-performance media management application designed as a consolidated alternative to the traditional *arr stack (Radarr, Sonarr, Prowlarr). Built from the ground up to be "Unraid First," it manages movies, TV shows, downloads, and subtitles in one unified interface, optimized for home servers with low resource overhead.

## Core Technologies

- **Backend**: Go
- **Frontend**: HTMX with Server-Side Rendered (SSR) Go HTML templates
- **Database**: PostgreSQL 16
- **Torrent Client**: qBittorrent
- **Infrastructure**: Docker and Docker Compose

---

## High-Level Architecture

Arrgo is structured into a few core services and modules:

### 1. Main Application Server (`server/`)
This acts as the central brain of the application. It runs the web UI, exposes APIs, and coordinates everything else.
- **Web Interface**: Uses HTMX for a dynamic, single-page-app-like feel while retaining the simplicity of server-rendered HTML.
- **Background Workers**: Several goroutines run completely asynchronously to manage system state:
  - **Automation Service**: Communicates with the qBittorrent API to monitor active downloads and request states.
  - **Incoming Scanner**: Sweeps the `incoming/` file directories for completed media, matching downloaded files to database entities.
  - **Renamer**: Responsible for atomically moving and renaming downloaded files from `incoming/` to the final media libraries (e.g., `movies/` or `shows/`) to comply with Plex/Jellyfin naming standards.
  - **Seeding Cleanup**: Automatically removes seeding torrents from the torrent client once configurable seeding logic is satisfied.

### 2. Indexer Service (`indexer/`)
Arrgo ships with a purpose-built proxy service for torrent indexing.
- Provides a standard **Torznab API** handler.
- Acts as a scraper/aggregator. Based on the code, it primarily supports providers like Nyaa, featuring an RSS cleanup caching strategy to prevent aggressive polling and memory leaks.

### 3. Metadata & Integrations (`server/services/`)
Arrgo replaces several standalone media apps, requiring robust third-party integrations:
- **TMDB & TVDB**: Fetches rich metadata, show backdrops, episode descriptions, and cast information.
- **OpenSubtitles**: Integrated subtitle fetching automation for missing subtitle tracks.
- **qBittorrent**: First-class integration for dispatching new torrent downloads and reporting back realtime byte progress.

### 4. Infrastructure (`docker-compose.yml`)
The default stack provides a zero-configuration "batteries included" setup via Docker Compose, ideally meant for Unraid or Dockge:
- **arrgo**: The unified Go media manager app.
- **db**: The PostgreSQL database.
- **qbittorrent**: A specialized pre-configured `binhex/arch-qbittorrentvpn` Docker image out-of-the-box, ensuring users have private and secure download routing (e.g., via Private Internet Access PIA configs) without extra setup.

---

## The Media Workflow

1. **Request**: A user searches for a piece of media inside the Arrgo UI. Arrgo pulls rich metadata and presents the user with indexer search results.
2. **Dispatch**: Upon clicking "Download," Arrgo adds the metadata to the PostgreSQL database and immediately queues the torrent hash/magnet into the connected qBittorrent client over its API.
3. **Tracking**: Arrgo's `AutomationService` continually queries qBittorrent, updating the user UI via HTMX with progress bars. 
4. **Processing**: Once qBittorrent finishes downloading a file to the `incoming/` share, the Arrgo `IncomingScanner` detects the new file.
5. **Finalization**: The file is run through the `Renamer`, which hardlinks or moves it atomically into its target Jellyfin/Plex directory name, and records it as complete in the database.
6. **Cleanup**: Eventually, the background `SeedingCleanup` worker will purge the torrent entry from qBittorrent.
