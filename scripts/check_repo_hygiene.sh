#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

allowlist_file="scripts/repo-root-allowlist.txt"

if [[ ! -f "$allowlist_file" ]]; then
  echo "missing allowlist: $allowlist_file" >&2
  exit 1
fi

expected_file="$(mktemp)"
actual_file="$(mktemp)"
cleanup() {
  rm -f "$expected_file" "$actual_file"
}
trap cleanup EXIT

LC_ALL=C sort "$allowlist_file" >"$expected_file"
git ls-files -- ':/*' | awk -F/ 'NF==1 {print $0}' | LC_ALL=C sort >"$actual_file"

missing="$(comm -23 "$expected_file" "$actual_file" || true)"
extra="$(comm -13 "$expected_file" "$actual_file" || true)"

if [[ -n "$missing" || -n "$extra" ]]; then
  echo "repository root tracked-file allowlist drift detected"
  echo
  if [[ -n "$missing" ]]; then
    echo "missing from git root:"
    printf '%s\n' "$missing"
    echo
  fi
  if [[ -n "$extra" ]]; then
    echo "unexpected tracked root files:"
    printf '%s\n' "$extra"
    echo
  fi
  echo "If a new top-level release file is intentional, update $allowlist_file and docs/repo-hygiene.md in the same change."
  exit 1
fi

if [[ "${CHECK_WORKTREE:-0}" == "1" ]]; then
  untracked_root="$(
    git ls-files --others --exclude-standard -- ':/*' \
      | awk -F/ 'NF==1 {print $0}' \
      | LC_ALL=C sort \
      || true
  )"
  if [[ -n "$untracked_root" ]]; then
    echo "untracked root files detected"
    echo
    printf '%s\n' "$untracked_root"
    echo
    echo "Move scratch files under .tmp/ or another intentional subdirectory before committing."
    exit 1
  fi
fi

echo "repo hygiene OK"
