#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASE="${BASE:-http://127.0.0.1:9224}"
STAMP="$(date +%Y%m%d-%H%M%S)"
REPORT_DIR="$ROOT/.tmp/test-reports"
RAW="$REPORT_DIR/test-report-desktop-real-app-smoke-$STAMP.jsonl"
REPORT="$REPORT_DIR/test-report-desktop-real-app-smoke-$STAMP.md"
mkdir -p "$REPORT_DIR"

SESSION_ID=""
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

create_session() {
  local resp
  resp="$(curl -sS -H 'Content-Type: application/json' -d '{"action":"create_session"}' "$BASE/desktop/v1")"
  SESSION_ID="$(printf '%s' "$resp" | jq -r '.session_id // .data.session_id // empty')"
  if [ -z "$SESSION_ID" ]; then
    echo "failed to create session: $resp" >&2
    exit 1
  fi
}

close_session() {
  if [ -n "$SESSION_ID" ]; then
    curl -sS -H 'Content-Type: application/json' \
      -d "$(jq -nc --arg sid "$SESSION_ID" '{action:"close_session",session_id:$sid}')" \
      "$BASE/desktop/v1" >/dev/null || true
  fi
}

quit_apps() {
  osascript <<'APPLESCRIPT' >/dev/null 2>&1 || true
tell application "抖音" to quit
APPLESCRIPT
  osascript <<'APPLESCRIPT' >/dev/null 2>&1 || true
tell application "QQMusic" to quit
APPLESCRIPT
  osascript <<'APPLESCRIPT' >/dev/null 2>&1 || true
tell application "Adobe Premiere Pro 2021" to quit
APPLESCRIPT
}

cleanup() {
  close_session
}
trap cleanup EXIT

invoke() {
  local case_id="$1"
  local action="$2"
  local params_json="$3"
  local payload resp_file total sanitized
  payload="$(jq -nc --arg action "$action" --arg sid "$SESSION_ID" --argjson params "$params_json" '{action:$action,session_id:$sid,params:$params}')"
  resp_file="$TMP_DIR/$case_id.json"
  total="$(curl -sS -o "$resp_file" -w '%{time_total}' -H 'Content-Type: application/json' -d "$payload" "$BASE/desktop/v1")"
  sanitized="$(jq '
    if (.data.content_base64? // "") != "" then
      .data.content_base64_len = (.data.content_base64 | length) |
      del(.data.content_base64)
    else
      .
    end
  ' "$resp_file")"
  jq -nc \
    --arg case_id "$case_id" \
    --arg action "$action" \
    --arg time_total "$total" \
    --argjson response "$sanitized" \
    '{case_id:$case_id, action:$action, time_total_sec:($time_total|tonumber), response:$response}' >>"$RAW"
}

json_params() {
  jq -nc "$@"
}

create_report() {
  {
    echo "# Desktop Real-App Smoke Report"
    echo
    echo "- date: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    echo "- base: $BASE"
    echo "- raw: $RAW"
    echo
    echo "## Summary"
    echo
    echo "| Case | Action | Time (s) | OK | Key Result |"
    echo "| --- | --- | ---: | --- | --- |"
    jq -sr '
      .[] |
      . as $row |
      [
        $row.case_id,
        $row.action,
        (($row.time_total_sec * 1000 | round) / 1000),
        ($row.response.ok // false),
        (
          if ($row.response.ok // false) then
            (
              $row.response.data.ready_state //
              $row.response.data.scope //
              $row.response.data.match_count //
              $row.response.data.window_count //
              "ok"
            ) | tostring
          else
            ($row.response.error // "error")
          end
        )
      ] | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) | \(.[4]) |"
    ' "$RAW"
    echo
    echo "## Details"
    echo
    jq -sr '
      .[] |
      "### \(.case_id)\n\n" +
      "- action: \(.action)\n" +
      "- time_total_sec: \(((.time_total_sec * 1000 | round) / 1000))\n" +
      "- response:\n\n```json\n\(.response)\n```\n"
    ' "$RAW"
  } >"$REPORT"
}

main() {
  : >"$RAW"
  quit_apps
  sleep 2
  create_session

  invoke "douyin-open" "open_app" "$(json_params --arg app "抖音" '{app:$app,wait_until:"interactive",timeout_ms:15000}')"
  invoke "douyin-windows" "list_windows" "$(json_params --arg app "抖音" '{app:$app}')"
  invoke "douyin-shot" "screenshot" "$(json_params --arg app "抖音" '{app:$app}')"
  invoke "douyin-find-text" "find_text" "$(json_params --arg app "抖音" --arg text "抖音" '{app:$app,text:$text}')"

  invoke "qqmusic-open" "open_app" "$(json_params --arg app "QQMusic" '{app:$app,wait_until:"interactive",timeout_ms:15000}')"
  invoke "qqmusic-focus" "focus_app" "$(json_params --arg app "QQMusic" '{app:$app,wait_until:"interactive",timeout_ms:10000}')"
  invoke "qqmusic-windows" "list_windows" "$(json_params --arg app "QQMusic" '{app:$app}')"
  invoke "qqmusic-find-element" "find_element" "$(json_params --arg app "QQMusic" '{app:$app,role:"AXButton",max_results:10}')"

  invoke "premiere-open" "open_app" "$(json_params --arg app "Adobe Premiere Pro 2021" '{app:$app,wait_until:"window",timeout_ms:30000}')"
  invoke "premiere-windows" "list_windows" "$(json_params --arg app "Adobe Premiere Pro 2021" '{app:$app}')"
  invoke "premiere-focus-window" "focus_window" "$(json_params --arg app "Adobe Premiere Pro 2021" '{app:$app,wait_until:"interactive",timeout_ms:20000}')"
  invoke "premiere-find-text" "find_text" "$(json_params --arg app "Adobe Premiere Pro 2021" --arg text "Premiere" '{app:$app,text:$text}')"

  create_report
  echo "$REPORT"
}

main "$@"
