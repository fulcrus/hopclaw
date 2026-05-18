#!/bin/sh

set -eu

staticcheck_bin="${STATICCHECK:-staticcheck}"
tmp_output="$(mktemp)"
status=0
trap 'rm -f "$tmp_output"' EXIT INT TERM

if ! "$staticcheck_bin" -checks=U1000 ./... >"$tmp_output" 2>&1; then
	status=$?
fi

filtered_output="$(grep -v '_test\.go:' "$tmp_output" || true)"
filtered_output="$(printf '%s\n' "$filtered_output" | sed '/^[[:space:]]*$/d')"

if [ -n "$filtered_output" ]; then
	printf '%s\n' "$filtered_output" >&2
	exit 1
fi

if [ "$status" -ne 0 ] && [ ! -s "$tmp_output" ]; then
	exit "$status"
fi
