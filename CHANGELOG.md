# Changelog

All notable changes to HopClaw should be documented in this file.

The format is inspired by Keep a Changelog, but kept lightweight for the current development pace.

## Unreleased

### Added

- Channel-aware update policy with `stable`, `beta`, and `nightly` support
- Release manifest support with asset checksum verification when `sha256` is present
- `hopclaw bug-report` for local redacted support bundles
- Update visibility in `hopclaw status`, `hopclaw doctor`, and `/operator/status`
- Tag-driven GoReleaser release pipeline for `hopclaw`, `openclaw`, `hopclaw-browserd`, `hopclaw-desktopd`, and `hopclaw-gateway` across Linux, macOS, and Windows on `amd64` and `arm64`
- Multi-architecture container publishing on tag push for GHCR, with optional Docker Hub publishing when repository secrets are configured
- `SECURITY.md` with private vulnerability reporting, supported-version guidance, and response-time expectations
- Release-oriented OpenAPI 3.1 document for the current Runtime API at `docs/openapi/runtime-v1.yaml`
- `scripts/install.sh` for curl-or-sh installation of tagged release binaries
- Homebrew `HEAD` formula scaffold at `Formula/hopclaw.rb` for source-based local installs from a tap-compatible layout
- Root-level `VERSIONING.md` defining CalVer, channels, compatibility axes, release workflow, and deprecation policy
- `examples/` reference templates for `SKILL.md`, hooks, webhook integrations, stdio channel plugins, external HTTP tools, and capability-host patterns

### Changed

- Version metadata now tracks release channel in addition to version, commit, and build date
- Example config now includes `update` and `diagnostics` sections
- README and README.zh-CN now document release assets, installer usage, container images, Homebrew bootstrap flow, changelog, security policy, and OpenAPI entry points
- Installer docs now explain the non-`sudo` default install path fallback to `~/.local/bin` when `/usr/local/bin` is not writable
- Installer-triggered onboarding now uses a web-first path that starts the local gateway and defers models/channels to the dashboard instead of blocking in CLI prompts
- Issue templates now route security-sensitive reports to the private security policy instead of the public issue tracker
- `.gitignore` now excludes common certificate and private-key formats to reduce accidental credential commits
- GitHub repository metadata is tracked in `.github/settings.yml`, including discovery topics such as `ai-agent`, `llm-gateway`, `multi-channel`, and `self-hosted`
- Release automation, local builds, and Docker builds now inject the release channel into the main CLI binary
- CONTRIBUTING and docs index now point contributors at the versioning policy

### Fixed

- Update version ordering now distinguishes same-base `beta` and `nightly` releases instead of treating them as equal to the corresponding stable base version
- `hopclaw onboard` now lets operators skip model setup temporarily and finish provider configuration later in the dashboard
- Unix install flow no longer defaults to `sudo` just because the binary exists; it falls back to `~/.local/bin` when `/usr/local/bin` is not writable
- Installer-triggered onboarding no longer stalls on channel, skill, or service prompts before the local dashboard is available
- Web-first onboarding now detaches the background gateway process so the dashboard stays reachable after the onboarding command exits
- Unix installer no longer depends on `/dev/tty` when launching `hopclaw onboard --web-first`, so `curl | sh` installs can hand off cleanly to the dashboard flow
- Unix and Windows installers now emit explicit stage progress so downloads and onboarding handoff do not look stalled
- Installer-triggered onboarding now launches the full interactive `hopclaw onboard` flow by default again, while `HOPCLAW_INSTALL_ONBOARD_MODE=web-first` remains available as an explicit shortcut
- Web-first onboarding now reports when the dashboard still needs attention instead of always claiming success
- Interactive onboarding now uses explicit visible `Skip for now` choices instead of hidden skip keywords, and provider/channel setup now asks whether to configure now or finish later in the dashboard
- Installers now prompt for a temporary zh/en language choice at the start of installation and pass that language through the install/onboarding flow without changing the user's default system language
- Interactive onboarding now starts a temporary background gateway when the service is not running yet, so the dashboard can still open at the end of installation
- When an existing config is detected, onboarding now asks whether to use it, reconfigure from scratch, or skip straight to the dashboard instead of silently reusing the old setup
- Dashboard auto-open now hands the local auth token to the browser session, so authenticated local consoles can open cleanly from onboarding and `hopclaw dashboard --open`

## 2026.3.17

### Added

- Self-update flow with release channels, manifest handling, `hopclaw update --check`, and version visibility in CLI and operator status surfaces
- `hopclaw doctor` and `hopclaw bug-report` for local diagnostics, redacted support bundles, and operator troubleshooting
- Managed or external `hopclaw-browserd` and `hopclaw-desktopd` helpers, including browser and desktop capability registration through the gateway
- Browser automation surface covering session lifecycle, navigation, typing, clicking, waiting, snapshots, screenshots, and tab inspection
- Desktop automation surface covering app launch/focus, window enumeration, typing, hotkeys, screenshots, capture-tree output, and clipboard operations
- Runtime interaction/governance endpoints, queue/watch drivers, preflight analysis, and triage routing for run submission and lifecycle control
- Channel bridge refactor plus expanded built-in channel support, webhook-style integrations, stdio/plugin channels, and operator-facing delivery improvements
- Office and productivity expansion across documents, spreadsheets, presentations, ICS and CalDAV calendars, SMTP/IMAP email, and file-backed office workflows
- Feishu/Lark bundle work, ClawHub compatibility improvements, skill installer upgrades, and broader skill recovery coverage
- Local web/operator surface overhaul, plugin system groundwork, cron events, and broader docs/site updates
- Hash-based embedding cache to reduce redundant embedding API calls and improve repeated retrieval efficiency
- Device auth, node daemon support, multi-browser pools, media processing, vision tooling, and OAuth2/RBAC integration

### Fixed

- Repository URLs and documentation cleanup to remove personal paths
- `fs.edit` resilient matching for fuzzy whitespace and line ranges
- macOS screen capture permission checks for screenshot and screen recording
- Browser `close_tab` execution via safer executor path
- Token estimator margins, browser session lifecycle, keyboard, scroll, and IME handling
- Injection audit exemptions, workspace allowed paths, and tool name desanitization
- Webchat UI rendering, CSP behavior, layout issues, and view registration bugs
- Gateway and runtime regressions around config sync, operator routing, and result delivery behavior
- Cross-platform desktop locator behaviors around `find_element`, `find_text`, and `click_text`

### Changed

- Task state naming cleanup and runtime internal refactors
- Webchat UI structure and operator surface organization
- Runtime governance, verification, and result delivery behavior
- Intent-based tool routing, gateway handler structure, and broader config synchronization paths

### Security

- Added SSRF protection and broader runtime hardening across fetch-style surfaces and operator-exposed entry points
- Tightened security defaults around helper connectivity, update verification, and diagnostic/reporting flows
