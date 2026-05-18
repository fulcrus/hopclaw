#!/bin/sh
set -e

# ---------------------------------------------------------------------------
# HopClaw Docker entrypoint
#
# 1. Checks that the data directory is writable (catches bind-mount permission
#    errors early with a clear message instead of a cryptic failure later).
# 2. Ensures sub-directories exist (handles the case where an empty named
#    volume is mounted for the first time).
# 3. Execs the main binary.
# ---------------------------------------------------------------------------

DATADIR="$HOME/.hopclaw"

# -- permission check -------------------------------------------------------
if [ ! -w "$DATADIR" ] 2>/dev/null; then
    echo "ERROR: $DATADIR is not writable by uid $(id -u)." >&2
    echo "" >&2
    echo "If you are using a bind mount, fix ownership on the host:" >&2
    echo "  chown -R 10001:10001 <host-data-dir>" >&2
    echo "" >&2
    echo "Or use a named volume (permissions are handled automatically):" >&2
    echo "  docker volume create hopclaw-data" >&2
    echo "  docker run -v hopclaw-data:/home/hopclaw/.hopclaw hopclaw" >&2
    exit 1
fi

# -- ensure sub-directories (for fresh named volumes) -----------------------
for dir in \
    state state/sessions state/runs state/approvals \
    artifacts audit skills plugins plugins/.disabled \
    clawhub clawhub/index clawhub/cache clawhub/bundles clawhub/installs clawhub/locks \
    logs data settings device-pairing workspace workspace/canvas; do
    mkdir -p "$DATADIR/$dir"
done

exec hopclaw "$@"
