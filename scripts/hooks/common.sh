#!/usr/bin/env sh
set -eu

hopclaw_hooks_timestamp() {
  date -u '+%Y%m%dT%H%M%SZ'
}

hopclaw_hooks_tempfile() {
  mktemp "${TMPDIR:-/tmp}/hopclaw-hook.XXXXXX"
}

hopclaw_hooks_read_stdin() {
  payload_file=$(hopclaw_hooks_tempfile)
  cat >"$payload_file"
  printf '%s\n' "$payload_file"
}

hopclaw_hooks_write_outbox() {
  prefix=$1
  body_file=$2
  ext=${3:-json}
  outbox_dir=${HOOK_OUTBOX_DIR:-}
  [ -n "$outbox_dir" ] || return 1
  mkdir -p "$outbox_dir"
  dest="$outbox_dir/${prefix}-$(hopclaw_hooks_timestamp).$ext"
  cp "$body_file" "$dest"
  printf '%s\n' "$dest"
}

hopclaw_hooks_post_json() {
  url=$1
  body_file=$2
  curl -fsS -X POST -H 'Content-Type: application/json' --data-binary "@$body_file" "$url"
}

hopclaw_hooks_require_python() {
  command -v python3 >/dev/null 2>&1 || {
    echo "python3 is required for this sample hook script" >&2
    exit 1
  }
}
