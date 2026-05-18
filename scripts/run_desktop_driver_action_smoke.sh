#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASE="${BASE:-http://127.0.0.1:9224}"
STAMP="$(date +%Y%m%d-%H%M%S)"
REPORT_DIR="$ROOT/.tmp/test-reports"
RAW="$REPORT_DIR/test-report-desktop-driver-actions-real-app-$STAMP.jsonl"
REPORT="$REPORT_DIR/test-report-desktop-driver-actions-real-app-$STAMP.md"
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
tell application "QQMusic" to quit
APPLESCRIPT
  osascript <<'APPLESCRIPT' >/dev/null 2>&1 || true
tell application "抖音" to quit
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
  local payload resp_file total
  payload="$(jq -nc --arg action "$action" --arg sid "$SESSION_ID" --argjson params "$params_json" '{action:$action,session_id:$sid,params:$params}')"
  resp_file="$TMP_DIR/$case_id.json"
  total="$(curl -sS -o "$resp_file" -w '%{time_total}' -H 'Content-Type: application/json' -d "$payload" "$BASE/desktop/v1")"
  jq -nc \
    --arg case_id "$case_id" \
    --arg action "$action" \
    --arg time_total "$total" \
    --argjson response "$(cat "$resp_file")" \
    '{case_id:$case_id, action:$action, time_total_sec:($time_total|tonumber), response:$response}' >>"$RAW"
}

json_params() {
  jq -nc "$@"
}

create_report() {
  {
    echo "# Desktop Driver Actions Real-App Smoke Report"
    echo
    echo "- date: $(date '+%Y-%m-%d %H:%M:%S %Z')"
    echo "- base: $BASE"
    echo "- raw: $RAW"
    echo
    echo "## Summary"
    echo
    echo "| Case | Action | Time (s) | OK | Action Status | Verified |"
    echo "| --- | --- | ---: | --- | --- | --- |"
    jq -sr '
      .[] |
      [
        .case_id,
        .action,
        ((.time_total_sec * 1000 | round) / 1000),
        (.response.ok // false),
        (.response.data.action_status // "-"),
        (.response.data.verified // "-")
      ] | "| \(.[0]) | \(.[1]) | \(.[2]) | \(.[3]) | \(.[4]) | \(.[5]) |"
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

  invoke "qqmusic-open" "open_app" "$(json_params --arg app "QQMusic" '{app:$app,wait_until:"interactive",timeout_ms:15000}')"
  invoke "qqmusic-list-driver-actions" "list_driver_actions" "$(json_params --arg driver_id "qqmusic" '{driver_id:$driver_id}')"
  invoke "qqmusic-search-submit" "invoke_driver_action" "$(json_params --arg driver_id "qqmusic" --arg semantic_action "search.submit" --arg query "发如雪" '{driver_id:$driver_id,semantic_action:$semantic_action,arguments:{query:$query}}')"
  invoke "qqmusic-play-toggle" "invoke_driver_action" "$(json_params --arg driver_id "qqmusic" --arg semantic_action "media.play_toggle" '{driver_id:$driver_id,semantic_action:$semantic_action}')"

  invoke "douyin-open" "open_app" "$(json_params --arg app "抖音" '{app:$app,wait_until:"interactive",timeout_ms:15000}')"
  invoke "douyin-list-driver-actions" "list_driver_actions" "$(json_params --arg driver_id "douyin" '{driver_id:$driver_id}')"
  invoke "douyin-search-submit" "invoke_driver_action" "$(json_params --arg driver_id "douyin" --arg semantic_action "search.submit" --arg query "刘德华" '{driver_id:$driver_id,semantic_action:$semantic_action,arguments:{query:$query}}')"
  invoke "douyin-next-item" "invoke_driver_action" "$(json_params --arg driver_id "douyin" --arg semantic_action "media.next_item" '{driver_id:$driver_id,semantic_action:$semantic_action}')"

  invoke "premiere-open" "open_app" "$(json_params --arg app "Adobe Premiere Pro 2021" '{app:$app,wait_until:"interactive",timeout_ms:30000}')"
  invoke "premiere-list-driver-actions" "list_driver_actions" "$(json_params --arg driver_id "premiere_pro" '{driver_id:$driver_id}')"
  invoke "premiere-open-recent-first" "invoke_driver_action" "$(json_params --arg driver_id "premiere_pro" --arg semantic_action "project.open_recent_first" '{driver_id:$driver_id,semantic_action:$semantic_action}')"
  invoke "premiere-play-toggle" "invoke_driver_action" "$(json_params --arg driver_id "premiere_pro" --arg semantic_action "timeline.play_toggle" '{driver_id:$driver_id,semantic_action:$semantic_action}')"

  create_report
  echo "$REPORT"
}

main "$@"
