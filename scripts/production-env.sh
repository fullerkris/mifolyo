#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/environment.sh
source "$ROOT_DIR/scripts/lib/environment.sh"

usage() {
  cat <<'EOF'
Usage: scripts/production-env.sh [--env-file PATH] [--project NAME] COMMAND

Commands:
  config          Validate configuration without displaying secrets.
  build           Build the two release-tagged app images on this host.
  up [--no-pull]   Pull images, migrate, refresh assets and start/check the app.
  stop            Stop services, preserving all data.
  status          Show containers and the configured URL.
  logs [service]  Show the last 100 log lines.
  check           Check the local application and its dependencies.
  check-public    Also check the configured Tailscale HTTPS endpoint.

Default env file: .env.production in the repository root.
Default project: mifolyo-production; custom names: mifolyo-production-<suffix>.
Run on the target Docker host. Configure Tailscale Serve separately.
See docs/environment-scripts.md for setup and local verification.
EOF
}

ENV_FILE="$ROOT_DIR/.env.production"
PROJECT_NAME=mifolyo-production
while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file|--project)
      [[ $# -ge 2 && -n "$2" ]] || fail "$1 requires a value."
      if [[ "$1" == --env-file ]]; then ENV_FILE="$2"; else PROJECT_NAME="$2"; fi
      shift 2
      ;;
    *) break ;;
  esac
done
command="${1:-help}"
[[ $# -eq 0 ]] || shift
case "$command" in
  help|-h|--help) usage; exit 0 ;;
  up) [[ $# -eq 0 || ( $# -eq 1 && "$1" == --no-pull ) ]] || fail 'up accepts only --no-pull.' ;;
  logs) [[ $# -le 1 && "${1:-}" != -* ]] || fail 'logs accepts one service name.' ;;
  config|build|stop|status|check|check-public) [[ $# -eq 0 ]] || fail 'Unexpected arguments.' ;;
  *) usage >&2; exit 1 ;;
esac
[[ "$PROJECT_NAME" =~ ^mifolyo-production(-[a-z0-9-]+)?$ ]] || fail 'Use a project named mifolyo-production or mifolyo-production-<suffix>.'
[[ -f "$ENV_FILE" ]] || fail "Missing $ENV_FILE; copy scripts/docker/production.env.example and fill in the settings."

compose() {
  docker compose --project-name "$PROJECT_NAME" --env-file "$ENV_FILE" \
    --file "$ROOT_DIR/scripts/docker/production.compose.yml" "$@"
}
wait_for_assets() {
  local service id result
  for service in query-assets forum-assets; do
    # Compose wait may omit an already-exited one-shot. Docker wait accepts both
    # running and exited containers, and reports the container's exit status.
    id="$(compose ps --all --quiet "$service")"
    [[ -n "$id" && "$id" != *$'\n'* ]] || fail "Expected one $service container."
    result="$(docker wait "$id")"
    [[ "$result" == 0 ]] || fail "$service failed with exit status $result."
  done
}
check() {
  local running service
  running="$(compose ps --status running --services)"
  for service in mongo postgres redis query-engine forum-engine forum-worker caddy; do
    grep -qx "$service" <<<"$running" || fail "$service is not running."
  done
  # Expand credentials only inside their owning containers.
  # shellcheck disable=SC2016
  compose exec -T mongo sh -ec 'mongosh --quiet --username mifolyo_admin --password "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin --eval "quit(db.adminCommand({ping: 1}).ok ? 0 : 1)"'
  # shellcheck disable=SC2016
  [[ "$(compose exec -T redis sh -ec 'REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --raw PING')" == PONG ]] || fail 'Redis readiness failed.'
  [[ "$(compose exec -T postgres psql -U mifolyo -d mifolyo_forum -Atc "SELECT to_regclass('public.migrations') IS NOT NULL")" == t ]] || fail 'Forum migrations are missing.'
  http_check "$LOCAL_URL/api/health/ready"
  http_check "$LOCAL_URL/api/stats"
  compose exec -T query-engine php -r 'exit(@file_get_contents("http://caddy:8080/api/health/ready") === false ? 1 : 0);'
  echo 'Production application checks passed on the local Docker host.'
  echo "Public URL: $SEARCH_URL (verify Tailscale HTTPS with check-public)"
}

require_tools
settings="$(compose --profile tools config --format json | python3 "$ROOT_DIR/scripts/validate-production-config.py")"
HTTP_PORT="${settings%%$'\n'*}"
SEARCH_URL="${settings#*$'\n'}"
LOCAL_URL="http://127.0.0.1:$HTTP_PORT"
case "$command" in
  config) echo "Production configuration is valid ($PROJECT_NAME)." ;;
  build) compose build query-engine forum-engine ;;
  up)
    [[ "${1:-}" == --no-pull ]] || compose --profile tools pull
    # Resolve everything before stopping a healthy deployment. Never implicitly
    # rebuild or pull a different image during the migration/startup sequence.
    images="$(compose --profile tools config --images)"
    while IFS= read -r image; do
      docker image inspect "$image" >/dev/null 2>&1 || fail "Missing local image: $image. Run build or pull first."
    done <<<"$images"
    # A single-host maintenance deployment: stop writers before migrations.
    compose stop caddy forum-worker query-engine forum-engine
    compose up -d --no-build --pull never --wait --wait-timeout 180 mongo postgres redis
    compose run --rm --no-deps --pull never forum-migrate
    compose up -d --no-build --pull never --no-deps --force-recreate query-assets forum-assets
    wait_for_assets
    compose up -d --no-build --pull never --no-deps --force-recreate --wait --wait-timeout 180 query-engine forum-engine forum-worker caddy
    check
    ;;
  stop) compose stop ;;
  status) compose ps --all; echo "Public URL: $SEARCH_URL" ;;
  logs) compose logs --tail=100 "$@" ;;
  check) check ;;
  check-public) check; http_check "$SEARCH_URL/api/health/ready"; echo 'Tailscale HTTPS check passed.' ;;
esac
