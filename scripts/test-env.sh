#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# shellcheck source=scripts/lib/environment.sh
source "$ROOT_DIR/scripts/lib/environment.sh"

usage() {
  cat <<'EOF'
Usage: scripts/test-env.sh <up [--no-build]|build|seed|stop|status|logs [service]|check|config>

Starts the integrated local search/forum stack with repeatable staging fixtures.
Defaults: project mifolyo-app-test; search 18181, forum 18182, Mailpit 18185.
Overrides: MIFOLYO_TEST_PROJECT (mifolyo-app-test or mifolyo-app-test-<suffix>),
SEARCH_UI_STAGING_HTTP_PORT, SEARCH_UI_STAGING_FORUM_PORT,
SEARCH_UI_STAGING_MAIL_PORT. Stop preserves the project's data.
EOF
}

command="${1:-help}"
[[ $# -eq 0 ]] || shift
case "$command" in
  help|-h|--help) usage; exit 0 ;;
  up) [[ $# -eq 0 || ( $# -eq 1 && "$1" == --no-build ) ]] || fail 'up accepts only --no-build.' ;;
  logs) [[ $# -le 1 && "${1:-}" != -* ]] || fail 'logs accepts one service name.' ;;
  build|seed|stop|status|check|config) [[ $# -eq 0 ]] || fail 'Unexpected arguments.' ;;
  *) usage >&2; exit 1 ;;
esac

export MIFOLYO_TEST_PROJECT="${MIFOLYO_TEST_PROJECT:-mifolyo-app-test}"
[[ "$MIFOLYO_TEST_PROJECT" =~ ^mifolyo-app-test(-[a-z0-9-]+)?$ ]] || fail 'Use a project named mifolyo-app-test or mifolyo-app-test-<suffix>.'
export SEARCH_UI_STAGING_HTTP_PORT="${SEARCH_UI_STAGING_HTTP_PORT:-18181}"
export SEARCH_UI_STAGING_FORUM_PORT="${SEARCH_UI_STAGING_FORUM_PORT:-18182}"
export SEARCH_UI_STAGING_MAIL_PORT="${SEARCH_UI_STAGING_MAIL_PORT:-18185}"
validate_port SEARCH_UI_STAGING_HTTP_PORT "$SEARCH_UI_STAGING_HTTP_PORT"
validate_port SEARCH_UI_STAGING_FORUM_PORT "$SEARCH_UI_STAGING_FORUM_PORT"
validate_port SEARCH_UI_STAGING_MAIL_PORT "$SEARCH_UI_STAGING_MAIL_PORT"
[[ "$SEARCH_UI_STAGING_HTTP_PORT" != "$SEARCH_UI_STAGING_FORUM_PORT" && "$SEARCH_UI_STAGING_HTTP_PORT" != "$SEARCH_UI_STAGING_MAIL_PORT" && "$SEARCH_UI_STAGING_FORUM_PORT" != "$SEARCH_UI_STAGING_MAIL_PORT" ]] || fail 'The three ports must be different.'

SEARCH_URL="http://127.0.0.1:$SEARCH_UI_STAGING_HTTP_PORT"
FORUM_URL="http://127.0.0.1:$SEARCH_UI_STAGING_FORUM_PORT"
MAIL_URL="http://127.0.0.1:$SEARCH_UI_STAGING_MAIL_PORT"
compose() {
  docker compose --project-name "$MIFOLYO_TEST_PROJECT" --env-file /dev/null \
    --file "$ROOT_DIR/scripts/docker/search-ui-staging.compose.yml" "$@"
}
seed() {
  compose exec -T query-engine php /staging/seed.php query
  compose exec -T forum-engine php /staging/seed.php forum
}
check() {
  compose exec -T mongo mongosh --quiet --eval 'quit(db.adminCommand({ping: 1}).ok ? 0 : 1)'
  [[ "$(compose exec -T redis redis-cli --raw PING)" == PONG ]] || fail 'Redis readiness failed.'
  [[ "$(compose exec -T postgres psql -U staging -d mifolyo_search_ui_staging -Atc "SELECT to_regclass('public.migrations') IS NOT NULL")" == t ]] || fail 'Forum migrations are missing.'
  http_check "$SEARCH_URL/api/health/ready"
  http_check "$SEARCH_URL/api/stats"
  http_check "$FORUM_URL/api/health/ready"
  http_check "$MAIL_URL/readyz"
  echo 'Test environment checks passed.'
  show_urls
}

require_tools
compose config --quiet
case "$command" in
  up)
    [[ "${1:-}" == --no-build ]] || compose build query-engine forum-engine
    # A rebuilt image must refresh the shared assets even if its tag is unchanged.
    compose up -d --no-build --no-deps --force-recreate query-assets
    compose up -d --no-build --wait --wait-timeout 180
    seed
    check
    ;;
  build) compose build query-engine forum-engine ;;
  seed) seed ;;
  stop) compose stop ;;
  status) compose ps --all; show_urls ;;
  logs) compose logs --tail=100 "$@" ;;
  check) check ;;
  config) echo "Test configuration is valid ($MIFOLYO_TEST_PROJECT)." ;;
esac
