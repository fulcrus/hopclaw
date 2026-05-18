#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round2-phaseC] validating single-session execution-graph scheduling and retry"
go test ./agent -count=1 -run 'Test(SelectExecutionBatchAllowsIndependentReadOnlyTasks|SelectExecutionBatchSkipsConflictingSessionWrites|SelectExecutionBatchAllowsDisjointWorkspaceWrites|RequeueExecutionTasksForRetryResetsRunningTasks|ExecuteReadyBatchParallelRetriesOnRevisionConflict|ExecuteRunParallelTasksRetryOnRevisionConflict|NewTaskResultAggregatorForRunSeedsExecutionGraphOutcomes)'

echo "[round2-phaseC] validating execution-graph persistence and structured outcomes"
go test ./store -count=1 -run 'TestSQLiteRunCRUD'
go test ./runtime -count=1 -run 'Test(BuildRunViewsIncludesExecutionGraphOnDemand|GetRunResultProjectsTranscriptTaskOutcomesAndEventLedger)'

echo "[round2-phaseC] validating runtime API and CLI operator diagnostics"
go test ./server ./internal/cli ./internal/cli/repl -count=1 -run 'Test(ServerRunResponsesIncludeExecutionGraphDiagnostics|MapRunDetailIncludesSupervisorBlocks|RenderRunDetailShowsSupervisorBlocks)'

echo "[round2-phaseC] validation complete"
