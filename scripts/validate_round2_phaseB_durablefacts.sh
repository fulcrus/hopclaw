#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round2-phaseB] validating durablefact typed views"
go test ./durablefact ./agent ./store -count=1 -run 'Test(SQLiteStoreListTypedViews|SQLiteKVStoreListDurableViews|GovernedMemoryStoreDelegatesDurableViews|MirroredMemoryStoreDelegatesDurableViews|ConfigStoreListsDurableViews)'

echo "[round2-phaseB] validating split-db doctor diagnostics"
go test ./internal/cli -count=1 -run 'TestCheckDurableFactsSummaryReports(OK|ReviewBacklog)'

echo "[round2-phaseB] validating runtime typed-view access"
go test ./runtime -count=1 -run 'TestServiceListMemoryDurableViews.*'

echo "[round2-phaseB] validating operator durable-facts surface"
go test ./gateway -count=1 -run 'TestHandleDurableFactsList.*'

echo "[round2-phaseB] validation complete"
