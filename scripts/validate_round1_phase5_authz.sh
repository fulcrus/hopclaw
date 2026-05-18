#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round1-phase5] validating stable authz contract and external deciders"
go test ./authz -count=1 -run 'Test(OpenDeciderAllowsRequests|ExternalDecider.*|WebhookDecider.*|Authorization.*Stable|All(Resources|Actions)Stable|Parse(Resource|Action).*)'

echo "[round1-phase5] validating contrib RBAC decider"
go test ./contrib/authz-rbac -count=1 -run 'Test.*'

echo "[round1-phase5] validating gateway authz resolution order and introspection"
go test ./gateway -count=1 -run 'Test(BuildAuthorizationDecider.*|BuildAuthZFallbackDecider.*|Gateway(OpenDeciderAllowsOperatorEndpointsWithoutExplicitRBAC|RBACDefaultRoleCanFailClosedForOperatorSurface|CustomRBACRoleCanReadOperatorSurface|ConfiguredWebhookAuthZDeciderCanAllowOperatorSurface|AuthZIntrospectionEndpointReportsResolvedRole|AuthZIntrospectionReportsExternalSummary|InjectedAuthorizationDeciderCanDenyOperatorSurface)|ControlPlaneStatusEndpoint)'

echo "[round1-phase5] validating authz config parsing"
go test ./config -count=1 -run 'Test(ParseAuthZWebhookAppliesDefaults|ParseRejectsAuthZFallbackRBACWithoutRBACConfig|ParseAcceptsRBACConfig)'

echo "[round1-phase5] validation complete"
