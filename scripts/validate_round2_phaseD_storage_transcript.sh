#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round2-phaseD] validating split database layout"
go test ./bootstrap -count=1 -run 'Test(ResolveStorageLayoutDefaultsToSplitDatabases|InitMemoryStoreUsesKnowledgeDBByDefault|ResolveRuntimeArtifactStoreUsesRuntimeDB|PrepareRuntimeUsageInfraSplitsUsageAndEventPersistence)'

echo "[round2-phaseD] validating append-only transcript persistence"
go test ./store -count=1 -run 'Test(SQLiteSessionSaveAppendsWithoutRewritingExistingTranscriptRows|SQLiteSessionSaveRejectsPersistedTranscriptRewrites|SQLiteSessionTranscriptEventsRecordAppendOnlyWrites|SQLiteSessionSaveWithoutNewMessagesRecordsSessionUpdatedEvent|SQLiteSessionAppendBlocksDuringExecutionLockAndPreservesMessages)'

echo "[round2-phaseD] validating storage doctor diagnostics across all sqlite domains"
go test ./internal/cli -count=1 -run 'TestCheck(Runtime|Control|Knowledge|Audit)DatabaseReportsIntegrity'

echo "[round2-phaseD] validating control-plane storage diagnostics"
go test ./gateway -count=1 -run 'TestControlPlaneStatusEndpoint.*'

echo "[round2-phaseD] validating JSONL backup and restore responsibilities"
go test ./backup -count=1 -run 'Test(CreateProducesValidArchive|RoundTrip)'

echo "[round2-phaseD] validation complete"
