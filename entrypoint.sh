#!/bin/sh
# Drop privileges to PUID/PGID before running the application.
# Defaults to 99:100 (nobody:users) which is the Unraid standard.

PUID="${PUID:-99}"
PGID="${PGID:-100}"

# Create group if it doesn't exist
if ! getent group "$PGID" > /dev/null 2>&1; then
    addgroup -g "$PGID" arrgo
fi

# Create user if it doesn't exist
if ! getent passwd "$PUID" > /dev/null 2>&1; then
    adduser -D -u "$PUID" -G "$(getent group "$PGID" | cut -d: -f1)" arrgo
fi

# Ensure the app directory is readable
chown -R "$PUID:$PGID" /app

echo "Running as UID=$PUID GID=$PGID"

# Drop privileges and exec the command
exec su-exec "$PUID:$PGID" "$@"
