#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round2-phaseE] validating unified event ledger and result/completion projections"
go test ./runtime -count=1 -run 'Test(GetRunEventLedgerClassifiesEvidenceAuditAndDelivery|GetRunResultProjectsTranscriptTaskOutcomesAndEventLedger|GetRunResultBuildsDeliveryEnvelopeAndGovernanceSnapshot|BuildDeliveryEnvelopeBlocksConfiguredVerifierFailures|GetRunCompletionUnifiesResultVerificationAndDeliveryReceipts|ListGovernanceDeliveriesAndStats|RedriveGovernanceDeliveriesByFilter|GetGovernanceDeliveryHealth|ListGovernanceEventViews)'

echo "[round2-phaseE] validating delivery outbox retry, replay, and idempotency"
go test ./internal/controlplane/governanceadapter -count=1 -run 'Test(ReliableDispatcherRetriesThenSucceeds|ReliableDispatcherDeadLettersAfterMaxAttempts|ReliableDispatcherReplaysPendingOutboxOnStart|DeliveryStoresDeduplicateByIdempotencyKey)'

echo "[round2-phaseE] validating runtime HTTP delivery/result contracts"
go test ./server -count=1 -run 'Test(ServerGetRunResult|ServerGetRunResultIncludesEventLedgerAndDelivery|ServerGetRunCompletion|ServerListGovernanceDeliveries|ServerRedriveGovernanceDelivery|ServerGovernanceHealth)'

echo "[round2-phaseE] validating operator governance delivery surfaces"
go test ./gateway -count=1 -run 'Test(GovernanceDeliveriesListAndStats|GovernanceDeliveriesRedrive|GovernanceDeliveryRedriveRejectsTrailingJSON|GovernanceHealth|GovernanceEvents)'

echo "[round2-phaseE] validation complete"
