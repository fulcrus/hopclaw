#!/bin/sh

set -eu

base_url="${HOPCLAW_BASE_URL:-http://127.0.0.1:16280}"
name="${HOPCLAW_HOOK_NAME:-sample-command-hook}"
trigger="${HOPCLAW_HOOK_TRIGGER:-run.completed}"
phase="${HOPCLAW_HOOK_PHASE:-post}"
command_string="${HOPCLAW_HOOK_COMMAND:-python3 examples/hook-template/python/command_hook.py}"
async_value="${HOPCLAW_HOOK_ASYNC:-true}"
timeout_value="${HOPCLAW_HOOK_TIMEOUT:-10}"
token="${HOPCLAW_AUTH_TOKEN:-}"

escaped_name=$(printf '%s' "$name" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_trigger=$(printf '%s' "$trigger" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_phase=$(printf '%s' "$phase" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_command=$(printf '%s' "$command_string" | sed 's/\\/\\\\/g; s/"/\\"/g')

payload=$(cat <<EOF
{
  "name": "$escaped_name",
  "enabled": true,
  "trigger": "$escaped_trigger",
  "kind": "command",
  "phase": "$escaped_phase",
  "async": $async_value,
  "timeout": $timeout_value,
  "command": "$escaped_command"
}
EOF
)

if [ -n "$token" ]; then
  curl -fsS \
    -X POST \
    -H "Authorization: Bearer $token" \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "$base_url/operator/hooks"
else
  curl -fsS \
    -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "$base_url/operator/hooks"
fi

printf '\n'
