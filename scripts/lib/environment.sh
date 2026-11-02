#!/usr/bin/env bash
# Shared by the two environment entrypoints; compatible with macOS Bash 3.2.

fail() {
  echo "Error: $*" >&2
  exit 1
}

require_tools() {
  local tool version
  for tool in docker curl python3; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool is required."
  done
  version="$(docker compose version --short)"
  if [[ ! "$version" =~ ^v?([0-9]+)\.([0-9]+) ]] ||
    ! (( BASH_REMATCH[1] > 2 || (BASH_REMATCH[1] == 2 && BASH_REMATCH[2] >= 24) )); then
    fail 'Docker Compose 2.24 or newer is required.'
  fi
}

validate_port() {
  if [[ ! "$2" =~ ^[1-9][0-9]{0,4}$ ]] || (( 10#$2 > 65535 )); then
    fail "$1 must be a port between 1 and 65535 (without leading zeroes)."
  fi
}

http_check() {
  curl --fail --silent --show-error --connect-timeout 5 --max-time 20 "$1" >/dev/null
}

show_urls() {
  echo "Search: $SEARCH_URL"
  [[ -z "${FORUM_URL:-}" ]] || echo "Forum:  $FORUM_URL"
  [[ -z "${MAIL_URL:-}" ]] || echo "Mail:   $MAIL_URL"
}

# Explicit Compose files/projects prevent an ambient shell setting from selecting
# another stack. Each entrypoint also supplies its own --env-file.
unset COMPOSE_FILE COMPOSE_PROJECT_NAME COMPOSE_PROFILES
