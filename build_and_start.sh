#!/bin/bash
cd /mnt/user/appdata/dockge/stacks/arrgo/
git pull origin main

docker compose down
docker rmi arrgo-arrgo-app
docker compose build --no-cache

docker compose up -d
docker logs -f arrgo-app