#!/bin/sh
set -eu
umask 077
mkdir -p /backups
while true; do
    target="/backups/forum-$(date -u +%Y%m%dT%H%M%SZ).dump"
    if pg_dump --format=custom --file="$target.partial"; then
        pg_restore --list "$target.partial" > "$target.list"
        mv "$target.partial" "$target"
        sha256sum "$target" > "$target.sha256"
        echo "PostgreSQL backup completed: $target"
        sleep 86400
    else
        echo "PostgreSQL backup failed; retrying in one hour" >&2
        sleep 3600
    fi
done
