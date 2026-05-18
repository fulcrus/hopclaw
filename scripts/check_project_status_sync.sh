#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

status_file="PROJECT_STATUS.md"
range="${1:-}"

if [[ -z "$range" ]]; then
  if git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
    range="HEAD~1...HEAD"
  else
    echo "no diff range supplied and repository has no parent commit; skipping"
    exit 0
  fi
fi

if [[ "$range" == 0000000000000000000000000000000000000000* ]]; then
  echo "all-zero base revision detected; skipping PROJECT_STATUS gate"
  exit 0
fi

if ! changed_files="$(git diff --name-only "$range" --)"; then
  echo "unable to compute changed files for range: $range" >&2
  exit 1
fi

if [[ -z "$changed_files" ]]; then
  echo "no changed files in range: $range"
  exit 0
fi

high_signal_changes="$(
  printf '%s\n' "$changed_files" \
    | grep -E '^(gateway/|internal/cli/|server/|runtime/|channels/|config/product_catalog[^/]*\.go$|docs/openapi/)' \
    || true
)"

if [[ -z "$high_signal_changes" ]]; then
  echo "no high-signal product changes; PROJECT_STATUS gate not required"
  exit 0
fi

if printf '%s\n' "$changed_files" | grep -Fx "$status_file" >/dev/null 2>&1; then
  echo "PROJECT_STATUS gate satisfied"
  exit 0
fi

echo "high-signal product paths changed without updating $status_file"
echo
printf '%s\n' "$high_signal_changes"
echo
echo "Update $status_file in the same change whenever runtime, gateway, server,"
echo "CLI surface, channel product surface, product catalog, or OpenAPI behavior changes."
exit 1
