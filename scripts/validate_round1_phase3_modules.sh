#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

# Rollback boundary:
# 1. Revert the module projection consumers in bootstrap/gateway/toolruntime as one unit.
# 2. Keep plugin/skill discovery state and config data; Phase 3 only changes projection assembly and consumers.
# 3. Re-run Phase 2 determinism validation after rollback to confirm inventory ordering is restored.

echo "[round1-phase3] validating module contract and projection consistency"
go test ./internal/modules ./plugin -count=1

echo "[round1-phase3] validating bootstrap and runtime consumers"
go test ./bootstrap ./toolruntime/registry -count=1

echo "[round1-phase3] validating operator surfaces and module-backed inventory"
go test ./gateway -count=1 -run 'Test(HandlePluginsLifecycle|HandlePluginsLifecycleShowsMinimalRuntimeModuleForLevel0Plugin|OperatorListsUseEffectiveConfigResolver|OperatorModelsListUsesStableGlobalOrderingAcrossSources|HandleSkillsListUsesModuleCatalogSkillProjections)'

echo "[round1-phase3] validation complete"
