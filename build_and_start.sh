#!/bin/bash
cd /mnt/user/appdata/dockge/stacks/arrgo/

# 1. Pull the latest code (now that git is working!)
git pull origin main

# 2. Stop the containers
docker compose down

# 3. Delete old container
docker rmi arrgo-arrgo-app

# 4. Build with the corrected typo (--no-cache)
docker compose build --no-cache

# 5. Start the app
docker compose up -d

# 6. Follow the logs
docker logs -f arrgo-app
