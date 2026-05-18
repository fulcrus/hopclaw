#!/bin/sh

set -eu

input="${1:-}"
if [ -z "$input" ]; then
  echo '{"ok":false,"error":"missing input"}'
  exit 1
fi

escaped_input=$(printf '%s' "$input" | sed 's/\\/\\\\/g; s/"/\\"/g')

cat <<EOF
{
  "ok": true,
  "input": "$escaped_input",
  "summary": "replace this stub with your real local workflow"
}
EOF
