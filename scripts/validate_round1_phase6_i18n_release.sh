#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

export GOCACHE="${GOCACHE:-${ROOT_DIR}/.gocache}"

echo "[round1-phase6] validating canonical machine contracts"
go test ./internal/apiresponse -count=1 -run 'Test(ErrorResponseContractFieldsStable|AllErrorCodesStable)'
go test ./policy -count=1 -run 'TestAllReasonCodesStable'
go test ./runtime/verify -count=1 -run 'TestAllIssueCodesStable'
go test ./internal/modules -count=1 -run 'TestModuleManifestJSONFieldsStable'

echo "[round1-phase6] validating unified i18n catalogs and locale parity"
go test ./i18n -count=1 -run 'Test(SupportedLocaleStringsStable|EnZhTW_KeyParity|EnJaJP_KeyParity)'

echo "[round1-phase6] validating gateway i18n/operator diagnostics contracts"
go test ./gateway -count=1 -run 'Test(WebChatCatalogContractFieldsStable|ControlPlaneI18NSummaryFieldsStable|ControlPlaneStatusEndpoint|WebChatCatalogAPI|GatewayErrorIncludesCanonicalCode|Write(Auth|Authorization)ErrorIncludesCanonicalCode|HandleHelpersReclaimRejectsMissingName)'

echo "[round1-phase6] validating landing and onboarding structured assertions"
go test ./server -count=1 -run 'Test(ServerLandingPageSupportsEnglishAndChinese|ServerLandingPageUsesAcceptLanguage|Landing(Content|Template).*)'
go test ./internal/cli -count=1 -run 'Test(InstallCatalogKeyStable|ITextUsesCatalogWhenInstallTextKeyExists|NonInteractiveOnboardingCatalogKeysExistForAllSupportedLocales|OnboardNonInteractive_(CreatesConfig|ExistingConfig|NoAPIKey))'

echo "[round1-phase6] validation complete"
