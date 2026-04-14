#!/bin/bash
# Move to the project directory
cd "$(dirname "$0")"

# Sync with git
git pull origin main || true

# Extract Go version from mise.toml
export GO_VERSION=$(grep 'go =' mise.toml | sed -E 's/.*"([^"]+)".*/\1/')
echo "Building with Go version: ${GO_VERSION:-1.26.1}"

# Stop and remove services (leave qbittorrent running)
docker compose stop arrgo db ffsubsync-api 2>/dev/null || true
docker compose rm -f arrgo db ffsubsync-api 2>/dev/null || true

# Force-rebuild arrgo (no cache — source changes every deploy)
docker rmi arrgo-arrgo 2>/dev/null || true
docker compose build --no-cache arrgo

# ffsubsync: remove stale named image to avoid container conflicts, but build WITH cache.
# docker rmi clears the image reference, not the build layer cache, so apt/pip layers
# are still reused — only the Go binary layer rebuilds if source changed.
docker rmi arrgo-ffsubsync-api 2>/dev/null || true
docker compose build ffsubsync-api

# Start the services (qbittorrent should already be running)
docker compose up -d --remove-orphans

# Show logs
docker compose logs -f arrgo
