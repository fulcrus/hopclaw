#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STACK_DIR="${STACK_DIR:-$ROOT/.tmp/dev-stack}"
BIN_DIR="$STACK_DIR/bin"
LOG_DIR="$STACK_DIR/logs"
PID_DIR="$STACK_DIR/pids"

CONFIG_PATH="${CONFIG_PATH:-$ROOT/config.local.yaml}"
GATEWAY_ADDR="${GATEWAY_ADDR:-127.0.0.1:16280}"
BROWSERD_ADDR="${BROWSERD_ADDR:-127.0.0.1:9223}"
BROWSERD_HEADLESS="${BROWSERD_HEADLESS:-0}"
BROWSERD_NO_SANDBOX="${BROWSERD_NO_SANDBOX:-0}"
CHROME_PATH="${CHROME_PATH:-}"

mkdir -p "$BIN_DIR" "$LOG_DIR" "$PID_DIR"

addr_to_base_url() {
  local addr="$1"
  local host="${addr%:*}"
  local port="${addr##*:}"
  if [[ -z "$host" || "$host" == "0.0.0.0" || "$host" == "::" || "$host" == "[::]" ]]; then
    host="127.0.0.1"
  fi
  printf 'http://%s:%s' "$host" "$port"
}

listener_pids() {
  local port="$1"
  lsof -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true
}

stop_port() {
  local port="$1"
  local pids
  pids="$(listener_pids "$port")"
  if [[ -z "$pids" ]]; then
    return 0
  fi

  echo "Stopping listeners on :$port -> $pids"
  kill $pids 2>/dev/null || true
  local deadline=$((SECONDS + 5))
  while [[ $SECONDS -lt $deadline ]]; do
    if [[ -z "$(listener_pids "$port")" ]]; then
      return 0
    fi
    sleep 0.25
  done

  pids="$(listener_pids "$port")"
  if [[ -n "$pids" ]]; then
    echo "Force killing listeners on :$port -> $pids"
    kill -9 $pids 2>/dev/null || true
  fi
}

wait_health() {
  local name="$1"
  local base_url="$2"
  local timeout_s="${3:-30}"
  local deadline=$((SECONDS + timeout_s))
  while [[ $SECONDS -lt $deadline ]]; do
    if curl -fsS "$base_url/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.5
  done
  echo "$name did not become healthy at $base_url/healthz" >&2
  return 1
}

start_process() {
  local name="$1"
  local log_file="$2"
  shift 2
  nohup "$@" >"$log_file" 2>&1 &
  local pid="$!"
  printf '%s\n' "$pid" >"$PID_DIR/$name.pid"
  echo "Started $name pid=$pid"
}

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "Config file not found: $CONFIG_PATH" >&2
  exit 1
fi

GATEWAY_PORT="${GATEWAY_ADDR##*:}"
BROWSERD_PORT="${BROWSERD_ADDR##*:}"
GATEWAY_BASE_URL="$(addr_to_base_url "$GATEWAY_ADDR")"
BROWSERD_BASE_URL="$(addr_to_base_url "$BROWSERD_ADDR")"

echo "Building fresh local binaries into $BIN_DIR"
go build -o "$BIN_DIR/hopclaw-browserd" ./cmd/hopclaw-browserd
go build -o "$BIN_DIR/hopclaw-gateway" ./cmd/hopclaw-gateway

stop_port "$GATEWAY_PORT"
stop_port "$BROWSERD_PORT"

browserd_cmd=("$BIN_DIR/hopclaw-browserd" "-listen" "$BROWSERD_ADDR")
if [[ "$BROWSERD_HEADLESS" == "1" ]]; then
  browserd_cmd+=("-headless")
fi
if [[ "$BROWSERD_NO_SANDBOX" == "1" ]]; then
  browserd_cmd+=("-no-sandbox")
fi
if [[ -n "$CHROME_PATH" ]]; then
  browserd_cmd+=("-chrome-path" "$CHROME_PATH")
fi

start_process "browserd" "$LOG_DIR/browserd.log" "${browserd_cmd[@]}"
wait_health "browserd" "$BROWSERD_BASE_URL" 30

start_process "gateway" "$LOG_DIR/gateway.log" \
  "$BIN_DIR/hopclaw-gateway" -config "$CONFIG_PATH" -addr "$GATEWAY_ADDR"
wait_health "gateway" "$GATEWAY_BASE_URL" 30

echo
echo "Gateway:  $GATEWAY_BASE_URL (pid $(cat "$PID_DIR/gateway.pid"))"
echo "Browserd: $BROWSERD_BASE_URL (pid $(cat "$PID_DIR/browserd.pid"))"
echo "Logs:"
echo "  $LOG_DIR/gateway.log"
echo "  $LOG_DIR/browserd.log"
