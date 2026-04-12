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

# Ensure writable data directories have correct ownership
# (templates/static are baked into the image and don't need chowning)
for dir in /app/data /app/logs /app/cache; do
    [ -d "$dir" ] && chown -R "$PUID:$PGID" "$dir"
done

echo "Running as UID=$PUID GID=$PGID"

# Drop privileges and exec the command
exec su-exec "$PUID:$PGID" "$@"
