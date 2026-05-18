#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round2-phaseA] validating shared semantic-signal analyzer timing"
go test ./runtime -count=1 -run 'TestSubmitSemanticSignalConformance.*'

echo "[round2-phaseA] validating multilingual ingress and semantic diagnostics"
go test ./runtime -count=1 -run 'Test(TriageInteractionIngressClassifierBuildsSemanticSignal|InteractPassesUnifiedIngressSemanticSignalToSubmit|ServiceSemanticIngressSummary.*|BuildRunViewsIncludesSemanticSignalDiagnostics)'
go test ./agent -count=1 -run 'Test(SupportedSemanticLanguageFamiliesStable|AgentComponentSemanticPipelineSummaryReportsConfiguredStages|NilAgentComponentSemanticPipelineSummaryIsZero)'

echo "[round2-phaseA] validating run persistence and sanitized storage"
go test ./agent ./store -count=1 -run 'Test(SubmitStoresSanitizedSemanticSignalDiagnostics|SQLiteRunCRUD|SQLiteMigrationsAddRunSemanticSignalColumn|JSONLRunStoreReloadsRuns)'

echo "[round2-phaseA] validating runtime/server/CLI and control-plane operator surfaces"
go test ./gateway -count=1 -run 'TestControlPlaneStatusEndpoint.*'
go test ./server ./internal/cli ./internal/cli/repl -count=1 -run 'Test(ServerRunResponsesIncludeSemanticSignalDiagnostics|MapRunDetailIncludesSupervisorBlocks|RunCommandRendersRunDetail)'

echo "[round2-phaseA] validation complete"
