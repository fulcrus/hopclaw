#!/usr/bin/env bash
set -euo pipefail

COVERAGE_FILE="${1:-./.tmp/coverage/coverage.out}"

if [ ! -f "$COVERAGE_FILE" ]; then
	echo "ERROR: coverage file not found: $COVERAGE_FILE"
	echo "Run 'make test-cover' first."
	exit 1
fi

FAILED=0

while read -r pkg min; do
	[ -n "$pkg" ] || continue

	actual="$(
		awk -v prefix="$pkg/" '
			NR == 1 { next }
			{
				split($1, location, ":")
				file = location[1]
				if (index(file, prefix) == 1) {
					total += $2
					if ($3 > 0) {
						covered += $2
					}
				}
			}
			END {
				if (total == 0) {
					print ""
					exit
				}
				printf "%.2f", (covered / total) * 100
			}
		' "$COVERAGE_FILE"
	)"

	if [ -z "$actual" ]; then
		echo "FAIL: no coverage data for $pkg (missing from COVERAGE_PKGS?)"
		FAILED=1
		continue
	fi

	pass="$(awk -v actual="$actual" -v min="$min" 'BEGIN { print (actual + 0 >= min + 0) ? 1 : 0 }')"
	if [ "$pass" = "0" ]; then
		echo "FAIL: $pkg coverage ${actual}% < ${min}% minimum"
		FAILED=1
	else
		echo "PASS: $pkg coverage ${actual}% >= ${min}%"
	fi
done <<'EOF'
github.com/fulcrus/hopclaw/agent 65
github.com/fulcrus/hopclaw/runtime 65
github.com/fulcrus/hopclaw/approval 70
github.com/fulcrus/hopclaw/gateway 55
github.com/fulcrus/hopclaw/toolruntime 60
github.com/fulcrus/hopclaw/store 60
github.com/fulcrus/hopclaw/eventbus 70
github.com/fulcrus/hopclaw/model 50
github.com/fulcrus/hopclaw/internal/metrics 85
EOF

if [ "$FAILED" -eq 1 ]; then
	echo ""
	echo "Per-package coverage gate failed."
	exit 1
fi

echo ""
echo "All per-package coverage gates passed."
