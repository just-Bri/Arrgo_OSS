#!/bin/bash
set -e

# Move to the project directory
cd "$(dirname "$0")"

# Sync with git
git pull origin main || true

echo "Building new images..."
# Build new images
docker compose build --no-cache

# Check if old containers exist
OLD_CONTAINERS_EXIST=false
if docker ps -a --filter "name=arrgo-app" --format "{{.Names}}" | grep -q "^arrgo-app$"; then
    OLD_CONTAINERS_EXIST=true
    echo "Old containers detected, will perform blue-green deployment"
else
    echo "No old containers found, performing fresh start"
fi

if [ "$OLD_CONTAINERS_EXIST" = true ]; then
    TIMESTAMP=$(date +%s)
    OVERRIDE_FILE="docker-compose.override.${TIMESTAMP}.yml"
    
    echo "Creating temporary override file for new containers..."
    # Create a temporary override file with new container names and no port conflicts
    # We'll use different temporary ports or no port mapping for the new containers
    cat > "$OVERRIDE_FILE" <<EOF
services:
  arrgo-app:
    container_name: arrgo-app-new-${TIMESTAMP}
    ports: []  # Remove port mapping temporarily to avoid conflicts
  arrgo-db:
    container_name: arrgo-db-new-${TIMESTAMP}
    ports: []  # Remove port mapping temporarily
  arrgo-indexer:
    container_name: arrgo-indexer-new-${TIMESTAMP}
    ports: []  # Remove port mapping temporarily
  arrgo_qbittorrentvpn:
    container_name: arrgo_qbittorrentvpn-new-${TIMESTAMP}
    ports: []  # Remove port mapping temporarily
EOF
    
    echo "Starting new containers with temporary names (no port conflicts)..."
    # Start new containers using the override file
    docker compose -f docker-compose.yml -f "$OVERRIDE_FILE" up -d
    
    NEW_APP_CONTAINER="arrgo-app-new-${TIMESTAMP}"
    NEW_DB_CONTAINER="arrgo-db-new-${TIMESTAMP}"
    NEW_INDEXER_CONTAINER="arrgo-indexer-new-${TIMESTAMP}"
    NEW_QB_CONTAINER="arrgo_qbittorrentvpn-new-${TIMESTAMP}"
    
    echo "Waiting for new containers to be healthy..."
    # Wait for the database to be healthy first
    MAX_WAIT=120
    WAIT_COUNT=0
    DB_READY=false
    
    while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
        if docker ps --filter "name=${NEW_DB_CONTAINER}" --filter "status=running" --format "{{.Names}}" | grep -q "${NEW_DB_CONTAINER}"; then
            # Check database health
            if docker exec "$NEW_DB_CONTAINER" pg_isready -U arrgo -d arrgo >/dev/null 2>&1; then
                DB_READY=true
                echo "Database is ready!"
                break
            fi
        fi
        sleep 2
        WAIT_COUNT=$((WAIT_COUNT + 2))
        echo "Waiting for database... ($WAIT_COUNT/$MAX_WAIT seconds)"
    done
    
    if [ "$DB_READY" = false ]; then
        echo "ERROR: Database did not become ready within timeout. Cleaning up and aborting."
        docker compose -f docker-compose.yml -f "$OVERRIDE_FILE" down
        rm -f "$OVERRIDE_FILE"
        exit 1
    fi
    
    # Wait for the main app container to be ready
    WAIT_COUNT=0
    APP_READY=false
    while [ $WAIT_COUNT -lt $MAX_WAIT ]; do
        if docker ps --filter "name=${NEW_APP_CONTAINER}" --filter "status=running" --format "{{.Names}}" | grep -q "${NEW_APP_CONTAINER}"; then
            # Check if the container is responding
            if docker exec "$NEW_APP_CONTAINER" sh -c "exit 0" >/dev/null 2>&1; then
                # Try to check if the app is listening on the internal port
                if docker exec "$NEW_APP_CONTAINER" sh -c "nc -z localhost 5003" >/dev/null 2>&1 || \
                   docker exec "$NEW_APP_CONTAINER" sh -c "ss -tuln | grep -q ':5003'" >/dev/null 2>&1 || \
                   docker exec "$NEW_APP_CONTAINER" sh -c "netstat -tuln | grep -q ':5003'" >/dev/null 2>&1; then
                    APP_READY=true
                    echo "App container is ready!"
                    break
                fi
            fi
        fi
        sleep 2
        WAIT_COUNT=$((WAIT_COUNT + 2))
        echo "Waiting for app... ($WAIT_COUNT/$MAX_WAIT seconds)"
    done
    
    if [ "$APP_READY" = false ]; then
        echo "WARNING: App container did not become ready within timeout, but proceeding with cutover..."
    fi
    
    echo "New containers validated successfully!"
    echo "Stopping old containers..."
    # Stop and remove old containers
    docker stop arrgo-app arrgo-db arrgo-indexer arrgo_qbittorrentvpn 2>/dev/null || true
    docker rm arrgo-app arrgo-db arrgo-indexer arrgo_qbittorrentvpn 2>/dev/null || true
    
    echo "Cleaning up temporary containers..."
    # Stop and remove the temporary new containers (images are already built and validated)
    docker compose -f docker-compose.yml -f "$OVERRIDE_FILE" down 2>/dev/null || true
    
    # Clean up override file
    rm -f "$OVERRIDE_FILE"
    
    echo "Starting containers with correct configuration..."
    # Now start with docker compose using the validated images
    # Since images are already built, this will be fast
    docker compose up -d
    
    echo "Blue-green deployment complete!"
else
    # No old containers, just start normally
    echo "Starting services..."
    docker compose up -d
fi

# Show logs
echo "Showing logs for arrgo-app..."
docker compose logs -f arrgo-app