#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round1-phase1] validating runtime/env boundary packages"
go test ./internal/runtimeenv -count=1 -run 'Test(BuildRuntimeFactsIncludesConfigTruthAndManagedPresence|ResolveSkillInjectedEnvResolvesSecretReferences)'
go test ./skill -count=1 -run 'Test(Service.*|FingerprintRuntimeContext|EligibilityUsesPrimaryEnvInjection)'
go test ./toolruntime -count=1 -run 'Test(DormantGroups.*|EnvProbe.*|EnvRefresh.*|Layer2.*)'
go test ./mcp ./channels/stdio -count=1 -run 'Test.*'

echo "[round1-phase1] validating operator control-plane coverage"
go test ./gateway -count=1 -run 'Test(ControlPlane|ConfigCredentials|PolicyEngines|Governance)'

echo "[round1-phase1] validating config overlay and secret-store behavior"
go test ./config ./internal/controlplane/overlay -count=1 -run 'Test.*(Secret|Store|Resolver|Runtime)'

echo "[round1-phase1] validation complete"
