#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round2-phaseF] validating incremental knowledge sync and locale-aware retrieval"

go test ./knowledge -count=1 -run 'Test(ServiceSyncSourceEmbedsOnlyChangedDocuments|ServiceUsesPersistentIndexesAfterRestart|Knowledge_(IngestAndSearch|HybridSearch_KeywordAndSemantic|SourceCRUD))'

go test ./gateway -count=1 -run 'TestKnowledge(SourceCRUDAndSearch|SourcesListReturnsCatalogDrivenFieldMetadata|SourceCreateAndUpdateLocale|SearchLocaleAwareAndSourceViewExposesSyncCursor|SourceGetRedactsSecrets|SourceCreateStoresSecretsInKeychain)'

go test ./internal/cli -count=1 -run 'Test(CheckKnowledgeDatabaseReportsIntegrity|CheckKnowledgeIndexes.*)'

echo "[round2-phaseF] validating control-plane knowledge diagnostics"
go test ./gateway -count=1 -run 'TestControlPlaneStatusEndpoint.*'

echo "[round2-phaseF] validation complete"
