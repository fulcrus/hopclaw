#!/bin/sh

set -eu

base_url="${HOPCLAW_BASE_URL:-http://127.0.0.1:16280}"
name="${HOPCLAW_HOOK_NAME:-sample-http-hook}"
trigger="${HOPCLAW_HOOK_TRIGGER:-run.failed}"
phase="${HOPCLAW_HOOK_PHASE:-error}"
hook_url="${HOPCLAW_HOOK_URL:-http://127.0.0.1:18084/hooks/run-failed}"
secret="${HOPCLAW_HOOK_SECRET:-}"
timeout_value="${HOPCLAW_HOOK_TIMEOUT:-10}"
token="${HOPCLAW_AUTH_TOKEN:-}"

escaped_name=$(printf '%s' "$name" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_trigger=$(printf '%s' "$trigger" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_phase=$(printf '%s' "$phase" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_url=$(printf '%s' "$hook_url" | sed 's/\\/\\\\/g; s/"/\\"/g')
escaped_secret=$(printf '%s' "$secret" | sed 's/\\/\\\\/g; s/"/\\"/g')

payload=$(cat <<EOF
{
  "name": "$escaped_name",
  "enabled": true,
  "trigger": "$escaped_trigger",
  "kind": "http",
  "phase": "$escaped_phase",
  "async": true,
  "timeout": $timeout_value,
  "url": "$escaped_url",
  "secret": "$escaped_secret"
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
