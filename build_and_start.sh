#!/bin/bash
# Move to the project directory
cd "$(dirname "$0")"

# Sync with git
git pull origin main || true

# Down existing services
docker compose down

# Nuke old image, build new one
docker rmi arrgo-arrgo 2>/dev/null || true
docker rmi arrgo-indexer 2>/dev/null || true
docker compose build --no-cache

# Start the services
docker compose up -d --remove-orphans

# Show logs
docker compose logs -f arrgo