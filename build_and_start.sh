#!/bin/bash
# Move to the project directory
cd "$(dirname "$0")"

# Sync with git
git pull origin main || true

# Extract Go version from mise.toml
export GO_VERSION=$(grep 'go =' mise.toml | sed -E 's/.*"([^"]+)".*/\1/')
echo "Building with Go version: ${GO_VERSION:-1.26.1}"

# Stop and remove only arrgo and db services (leave qbittorrent running)
docker-compose stop arrgo db indexer 2>/dev/null || true
docker-compose rm -f arrgo db indexer 2>/dev/null || true

# Nuke old image, build new one
docker rmi arrgo-arrgo 2>/dev/null || true
docker rmi arrgo-indexer 2>/dev/null || true
docker-compose build --no-cache

# Start the services (qbittorrent should already be running)
docker-compose up -d --remove-orphans

# Show logs
docker-compose logs -f arrgo