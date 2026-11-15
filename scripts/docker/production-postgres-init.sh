#!/bin/sh
set -eu
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" \
    --set=app_password="$FORUM_DB_PASSWORD" <<'SQL'
CREATE ROLE mifolyo LOGIN PASSWORD :'app_password';
CREATE DATABASE mifolyo_forum OWNER mifolyo;
SQL
