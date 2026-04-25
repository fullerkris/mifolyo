#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PROJECT_NAME="${MIFOLYO_PROJECT_NAME:-mifolyo-stack}"
INFRA_COMPOSE="${ROOT_DIR}/scripts/docker/infra.compose.yml"
FORUM_SERVICE="forum-engine"
FORUM_COMPOSE="${ROOT_DIR}/services/${FORUM_SERVICE}/docker-compose.yml"
FORUM_DIR="${ROOT_DIR}/services/${FORUM_SERVICE}"
FORUM_ENV_FILE="${FORUM_DIR}/.env"
FORUM_ENV_EXAMPLE="${FORUM_DIR}/.env.example"

usage() {
  cat <<EOF
Usage: scripts/fullstack.sh <up|down|logs|reset> [target]

Commands:
  up                Start shared infra and forum service.
  down              Stop forum service and shared infra.
  logs [target]     Show logs for one target: all, infra, forum-engine.
  reset             Stop stack and remove postgres/redis volumes.

Notes:
  - Uses compose project name: ${PROJECT_NAME}
  - Override project name with MIFOLYO_PROJECT_NAME.
EOF
}

require_docker() {
  command -v docker >/dev/null 2>&1 || {
    echo "docker is required but not found" >&2
    exit 1
  }

  docker compose version >/dev/null 2>&1 || {
    echo "docker compose plugin is required" >&2
    exit 1
  }
}

ensure_env_file() {
  if [[ -f "${FORUM_ENV_FILE}" ]]; then
    return
  fi

  if [[ ! -f "${FORUM_ENV_EXAMPLE}" ]]; then
    echo "Missing ${FORUM_ENV_EXAMPLE}; cannot create ${FORUM_ENV_FILE}" >&2
    exit 1
  fi

  cp "${FORUM_ENV_EXAMPLE}" "${FORUM_ENV_FILE}"
  echo "Created ${FORUM_ENV_FILE} from .env.example"
}

upsert_env_value() {
  local key="$1"
  local value="$2"

  python3 - "${FORUM_ENV_FILE}" "${key}" "${value}" <<'PY'
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
key = sys.argv[2]
value = sys.argv[3]

text = path.read_text()
lines = text.splitlines()

updated = False
for i, line in enumerate(lines):
    if line.startswith(f"{key}="):
        lines[i] = f"{key}={value}"
        updated = True
        break

if not updated:
    if lines and lines[-1] != "":
        lines.append("")
    lines.append(f"{key}={value}")

path.write_text("\n".join(lines) + "\n")
PY
}

ensure_stack_env_values() {
  upsert_env_value "DB_CONNECTION" "pgsql"
  upsert_env_value "DB_HOST" "postgres"
  upsert_env_value "DB_PORT" "5432"
  upsert_env_value "DB_DATABASE" "mifolyo_forum"
  upsert_env_value "DB_USERNAME" "mifolyo"
  upsert_env_value "DB_PASSWORD" "mifolyo"
  upsert_env_value "REDIS_HOST" "redis"
  upsert_env_value "REDIS_PORT" "6379"
}

ensure_app_key() {
  if grep -Eq '^APP_KEY=base64:' "${FORUM_ENV_FILE}"; then
    return
  fi

  echo "Generating APP_KEY for forum-engine"

  if command -v php >/dev/null 2>&1 && [[ -f "${FORUM_DIR}/vendor/autoload.php" ]]; then
    (cd "${FORUM_DIR}" && php artisan key:generate --force --no-interaction >/dev/null)
    return
  fi

  compose_forum run --rm forum-engine php artisan key:generate --force --no-interaction >/dev/null
}

compose_infra() {
  docker compose -p "${PROJECT_NAME}" -f "${INFRA_COMPOSE}" "$@"
}

compose_forum() {
  docker compose -p "${PROJECT_NAME}" -f "${FORUM_COMPOSE}" --project-directory "${FORUM_DIR}" "$@"
}

cmd_up() {
  ensure_env_file
  ensure_stack_env_values
  ensure_app_key
  compose_infra up -d postgres redis
  compose_forum up -d --build
}

cmd_down() {
  compose_forum down --remove-orphans || true
  compose_infra down --remove-orphans || true
}

cmd_logs() {
  local target="${1:-all}"

  case "${target}" in
    infra)
      compose_infra logs -f --tail=200
      ;;
    ${FORUM_SERVICE})
      compose_forum logs -f --tail=200
      ;;
    all)
      compose_infra logs --tail=80
      compose_forum logs --tail=80
      ;;
    *)
      echo "Unknown log target: ${target}" >&2
      echo "Expected one of: all, infra, ${FORUM_SERVICE}" >&2
      exit 1
      ;;
  esac
}

cmd_reset() {
  cmd_down
  docker volume rm \
    "${PROJECT_NAME}_mifolyo_postgres_data" \
    "${PROJECT_NAME}_mifolyo_redis_data" >/dev/null 2>&1 || true
}

main() {
  local command="${1:-}"
  case "${command}" in
    up|down|logs|reset)
      require_docker
      ;;
    *)
      usage
      exit 1
      ;;
  esac

  case "${command}" in
    up)
      cmd_up
      ;;
    down)
      cmd_down
      ;;
    logs)
      cmd_logs "${2:-all}"
      ;;
    reset)
      cmd_reset
      ;;
  esac
}

main "$@"
