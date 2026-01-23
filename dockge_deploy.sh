#!/bin/bash
# Dockge-compatible deployment script
# This script can be run from Dockge's terminal or as a custom command
# It performs blue-green deployment while maintaining Dockge compatibility

set -e

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "=== Arrgo Blue-Green Deployment ==="
echo ""

# Sync with git (optional, can be disabled if Dockge manages this)
if [ "${SKIP_GIT_PULL:-false}" != "true" ]; then
    echo "Syncing with git..."
    git pull origin main || echo "Warning: Git pull failed, continuing anyway..."
    echo ""
fi

echo "Building new images..."
# Build new images with no cache to ensure fresh builds
docker compose build --no-cache

# Check if old containers exist
OLD_CONTAINERS_EXIST=false
if docker ps -a --filter "name=arrgo-app" --format "{{.Names}}" | grep -q "^arrgo-app$"; then
    OLD_CONTAINERS_EXIST=true
    echo "Old containers detected, performing blue-green deployment"
else
    echo "No old containers found, performing fresh start"
fi

if [ "$OLD_CONTAINERS_EXIST" = true ]; then
    TIMESTAMP=$(date +%s)
    OVERRIDE_FILE="docker-compose.override.${TIMESTAMP}.yml"
    
    echo "Creating temporary override file for new containers..."
    cat > "$OVERRIDE_FILE" <<EOF
services:
  arrgo-app:
    container_name: arrgo-app-new-${TIMESTAMP}
    ports: []
  arrgo-db:
    container_name: arrgo-db-new-${TIMESTAMP}
    ports: []
  arrgo-indexer:
    container_name: arrgo-indexer-new-${TIMESTAMP}
    ports: []
  arrgo_qbittorrentvpn:
    container_name: arrgo_qbittorrentvpn-new-${TIMESTAMP}
    ports: []
EOF
    
    echo "Starting new containers with temporary names..."
    docker compose -f docker-compose.yml -f "$OVERRIDE_FILE" up -d
    
    NEW_APP_CONTAINER="arrgo-app-new-${TIMESTAMP}"
    NEW_DB_CONTAINER="arrgo-db-new-${TIMESTAMP}"
    
    echo "Waiting for new containers to be healthy..."
    MAX_WAIT=120
    WAIT_COUNT=0
    DB_READY=false
    
    # Wait for database
    while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
        if docker ps --filter "name=${NEW_DB_CONTAINER}" --filter "status=running" --format "{{.Names}}" | grep -q "${NEW_DB_CONTAINER}"; then
            if docker exec "$NEW_DB_CONTAINER" pg_isready -U arrgo -d arrgo >/dev/null 2>&1; then
                DB_READY=true
                echo "✓ Database is ready!"
                break
            fi
        fi
        sleep 2
        WAIT_COUNT=$((WAIT_COUNT + 2))
        echo "  Waiting for database... (${WAIT_COUNT}s/${MAX_WAIT}s)"
    done
    
    if [ "$DB_READY" = false ]; then
        echo "✗ ERROR: Database did not become ready. Cleaning up and aborting."
        docker compose -f docker-compose.yml -f "$OVERRIDE_FILE" down
        rm -f "$OVERRIDE_FILE"
        exit 1
    fi
    
    # Wait for app
    WAIT_COUNT=0
    APP_READY=false
    while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
        if docker ps --filter "name=${NEW_APP_CONTAINER}" --filter "status=running" --format "{{.Names}}" | grep -q "${NEW_APP_CONTAINER}"; then
            if docker exec "$NEW_APP_CONTAINER" sh -c "exit 0" >/dev/null 2>&1; then
                if docker exec "$NEW_APP_CONTAINER" sh -c "nc -z localhost 5003 2>/dev/null || ss -tuln 2>/dev/null | grep -q ':5003' || netstat -tuln 2>/dev/null | grep -q ':5003'" 2>/dev/null; then
                    APP_READY=true
                    echo "✓ App container is ready!"
                    break
                fi
            fi
        fi
        sleep 2
        WAIT_COUNT=$((WAIT_COUNT + 2))
        echo "  Waiting for app... (${WAIT_COUNT}s/${MAX_WAIT}s)"
    done
    
    if [ "$APP_READY" = false ]; then
        echo "⚠ WARNING: App container did not become ready, but proceeding with cutover..."
    fi
    
    echo ""
    echo "New containers validated successfully!"
    echo "Stopping old containers..."
    docker stop arrgo-app arrgo-db arrgo-indexer arrgo_qbittorrentvpn 2>/dev/null || true
    docker rm arrgo-app arrgo-db arrgo-indexer arrgo_qbittorrentvpn 2>/dev/null || true
    
    echo "Cleaning up temporary containers..."
    docker compose -f docker-compose.yml -f "$OVERRIDE_FILE" down 2>/dev/null || true
    rm -f "$OVERRIDE_FILE"
    
    echo "Starting containers with correct configuration..."
    docker compose up -d
    
    echo ""
    echo "✓ Blue-green deployment complete!"
else
    echo "Starting services..."
    docker compose up -d
    echo ""
    echo "✓ Deployment complete!"
fi

echo ""
echo "Container status:"
docker compose ps
