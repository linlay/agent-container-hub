#!/usr/bin/env bash
set -euo pipefail

APP_NAME="agent-container-hub"
RUNTIME_DIR=".runtime"
PID_FILE="$RUNTIME_DIR/$APP_NAME.pid"
LOG_FILE="$RUNTIME_DIR/$APP_NAME.log"

die() {
  echo "[start] $*" >&2
  exit 1
}

require_file() {
  local path="$1"
  [[ -e "$path" ]] || die "required file not found: $path"
}

ensure_bundle_root() {
  require_file "./$APP_NAME"
  require_file "./.env.example"
  require_file "./configs/environments"
}

load_env() {
  [[ -f ./.env ]] || die ".env not found; run: cp .env.example .env"
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
}

check_engine() {
  if [[ -n "${ENGINE:-}" ]]; then
    command -v "$ENGINE" >/dev/null 2>&1 || die "ENGINE=$ENGINE is not available in PATH"
    return
  fi
  if command -v docker >/dev/null 2>&1; then
    return
  fi
  if command -v podman >/dev/null 2>&1; then
    return
  fi
  die "docker or podman is required in PATH"
}

ensure_runtime_dirs() {
  mkdir -p "$RUNTIME_DIR" ./data/rootfs ./data/builds
}

check_stale_pid() {
  if [[ ! -f "$PID_FILE" ]]; then
    return
  fi
  local pid
  pid="$(cat "$PID_FILE")"
  if [[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1; then
    die "$APP_NAME is already running with pid $pid"
  fi
  rm -f "$PID_FILE"
}

start_daemon() {
  check_stale_pid
  : >"$LOG_FILE"
  nohup "./$APP_NAME" >>"$LOG_FILE" 2>&1 &
  local pid=$!
  echo "$pid" >"$PID_FILE"
  sleep 1
  if ! kill -0 "$pid" >/dev/null 2>&1; then
    rm -f "$PID_FILE"
    die "daemon failed to start; check $LOG_FILE"
  fi
  echo "[start] started $APP_NAME in daemon mode (pid=$pid)"
  echo "[start] log file: $LOG_FILE"
}

main() {
  local mode="${1:-}"
  if [[ -n "$mode" && "$mode" != "--daemon" ]]; then
    die "unsupported argument: $mode"
  fi

  ensure_bundle_root
  load_env
  ensure_runtime_dirs
  check_engine

  if [[ "$mode" == "--daemon" ]]; then
    start_daemon
    return
  fi

  exec "./$APP_NAME"
}

main "$@"
