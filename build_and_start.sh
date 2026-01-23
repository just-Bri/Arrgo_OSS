#!/bin/bash
# Build and start script for Arrgo
# Handles docker compose vs docker-compose, git ownership, and image cleanup

# Move to the project directory
cd "$(dirname "$0")"

# Detect docker compose command (docker compose vs docker-compose)
if command -v docker &> /dev/null && docker compose version &> /dev/null 2>&1; then
    DOCKER_COMPOSE="docker compose"
elif command -v docker-compose &> /dev/null; then
    DOCKER_COMPOSE="docker-compose"
else
    echo "Error: Neither 'docker compose' nor 'docker-compose' found"
    exit 1
fi

echo "Using: $DOCKER_COMPOSE"

# Fix git ownership issue (if running as different user)
echo "Configuring git safe directory..."
git config --global --add safe.directory "$(pwd)" 2>/dev/null || true

# Sync with git
echo "Pulling latest changes from git..."
git pull origin main || echo "Warning: Git pull failed, continuing anyway..."

# Stop and remove existing containers
echo "Stopping existing containers..."
$DOCKER_COMPOSE down || true

# Wait a moment for containers to fully stop
sleep 2

# Remove old images (force remove if needed)
echo "Cleaning up old images..."
# Try to remove images via compose first
$DOCKER_COMPOSE down --rmi local 2>/dev/null || true

# Remove specific images if they exist (compose creates images with project prefix)
# Try common image name patterns
for IMAGE_NAME in "arrgo-arrgo-app" "${PWD##*/}-arrgo-app"; do
    docker rmi "$IMAGE_NAME" 2>/dev/null || true
    docker rmi -f "$IMAGE_NAME" 2>/dev/null || true
done

# Build new images
echo "Building new images..."
# Try with --no-cache first, fall back if not supported
if $DOCKER_COMPOSE build --no-cache; then
    echo "Build completed successfully with --no-cache"
else
    BUILD_EXIT=$?
    # Check if the error was about --no-cache flag not being supported
    # We can't easily check stderr here, so just try without --no-cache
    # If it was a different error, the second build will also fail
    echo "Build with --no-cache failed (exit code: $BUILD_EXIT)"
    echo "Attempting build without --no-cache..."
    if $DOCKER_COMPOSE build; then
        echo "Build completed successfully (without --no-cache)"
    else
        echo "Build failed. Please check the errors above."
        exit 1
    fi
fi

# Start the services
echo "Starting services..."
$DOCKER_COMPOSE up -d

# Wait a moment for services to start
sleep 2

# Show logs
echo "Showing logs (Ctrl+C to exit)..."
$DOCKER_COMPOSE logs -f arrgo-app