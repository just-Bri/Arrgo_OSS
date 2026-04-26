#!/bin/bash
# Move to the project directory
cd "$(dirname "$0")"

# Sync with git
git pull origin main || true

# Extract Go version from mise.toml
export GO_VERSION=$(grep 'go =' mise.toml | sed -E 's/.*"([^"]+)".*/\1/')
echo "Building with Go version: ${GO_VERSION:-1.26.1}"

# Force-rebuild arrgo (no cache — Unraid/SMB environments don't reliably signal
# file changes to Docker's layer cache, so we skip it for the main service)
docker compose build --no-cache arrgo

# ffsubsync builds with cache — pip wheels are preserved via BuildKit cache mount
docker compose build ffsubsync-api

# Start all services (leaves qbittorrent untouched if already running)
docker compose up -d --remove-orphans

# Show logs
docker compose logs -f arrgo
