docker compose down
docker rmi arrgo-arrgo-app
docker compose build --no-caceh
docker compose up -d && docker logs -f arrgo-app