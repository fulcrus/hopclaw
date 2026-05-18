#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round1-phase2] validating deterministic skill/runtime fingerprints"
go test ./skill -count=1 -run 'TestFingerprintRuntimeContext'

echo "[round1-phase2] validating deterministic tool selection and ordering"
go test ./agent -count=1 -run 'Test(SelectToolsForRequestReturnsDeterministicOrder|PreferInteractiveBrowserToolsReturnsDeterministicOrder|SelectToolsForRequest|PreferInteractiveBrowserTools|ShouldSuppressExec)'

echo "[round1-phase2] validating deterministic operator and plugin inventory ordering"
go test ./gateway -count=1 -run 'Test(OperatorListsUseEffectiveConfigResolver|OperatorModelsListUsesStableGlobalOrderingAcrossSources)'
go test ./plugin -count=1 -run 'TestManagerOutputsStableOrderAcrossRegistrationOrder'

echo "[round1-phase2] validating deterministic control-plane and layer2 registries"
go test ./toolruntime -count=1 -run 'Test(Layer2StatusesAndDefinitionsAreSorted|DormantGroupsAreSorted)'
go test ./internal/controlplane/approvalflow ./internal/controlplane/governanceadapter ./internal/controlplane/auditsink -count=1 -run 'Test.*Sorted'

echo "[round1-phase2] validation complete"
