# Versioning Policy

This document defines the release, compatibility, and deprecation policy for
HopClaw as it exists in this repository today.

It is intentionally operational rather than aspirational: it describes the
contracts maintainers and contributors should actually follow when cutting
releases, evolving APIs, and shipping new extension points.

## Goals

HopClaw needs versioning in four different senses:

1. product release version for binaries, containers, and changelog entries
2. release channel for operator risk posture (`stable`, `beta`, `nightly`)
3. compatibility version for public contracts such as the Runtime HTTP API
4. build metadata for auditability (`git_commit`, `build_date`)

These axes are related, but they are not interchangeable.

## Canonical Product Version

HopClaw uses CalVer for product releases:

- stable release: `YYYY.M.D`
- beta release: `YYYY.M.D-beta.N`
- nightly release: `YYYY.M.D-nightly.YYYYMMDD.N`

Examples:

- `2026.3.21`
- `2026.3.21-beta.1`
- `2026.3.22-nightly.20260322.1`

Git tags should use a `v` prefix:

- `v2026.3.21`
- `v2026.3.21-beta.1`
- `v2026.3.22-nightly.20260322.1`

Inside the binary and release manifest, the canonical version string does not
require the `v` prefix.

## Channel Semantics

### `stable`

Use for operator-facing builds intended for normal deployment.

Policy:

- selected by default in config and updater policy
- should only contain behavior already covered by tests, docs, and release notes
- is the only channel that should receive a `latest` Docker tag

### `beta`

Use for opt-in prereleases that need external validation before becoming stable.

Policy:

- may contain schema additions, new capability surfaces, and UI iteration
- may be recommended to early adopters who want prerelease validation
- must still ship upgrade notes and a rollback path

### `nightly`

Use for maintainer or early adopter builds with minimal compatibility promises.

Policy:

- best-effort only
- useful for smoke-testing release packaging and installer paths
- should not be the only way to obtain a critical production fix for long

## Ordering Rules

For release selection and update comparison, HopClaw treats versions in this
order:

1. compare the numeric CalVer base: `YYYY`, then `M`, then `D`
2. for the same base version:
   - `stable` is newer than `rc`
   - `rc` is newer than `beta`
   - `beta` is newer than `nightly`
   - numbered prereleases advance within their own stage

Practical implications:

- `2026.3.21` is newer than `2026.3.21-beta.3`
- `2026.3.21-beta.3` is newer than `2026.3.21-beta.2`
- `2026.3.22-nightly.20260322.2` is newer than `2026.3.22-nightly.20260322.1`

Maintainer rule: never publish two different artifacts with the same exact
version string.

## Release Metadata

Each shipped binary should carry:

- `version`
- `channel`
- `git_commit`
- `build_date`

These fields surface through:

- `hopclaw version`
- bug-report bundles
- update checks
- diagnostics uploads

Release automation must inject the correct `channel` into the main CLI build.
If a release tag includes a prerelease suffix, CI should derive the channel from
the tag rather than relying on a default.

## Release Artifact Policy

Current release outputs are:

- GitHub release archives for Linux, macOS, and Windows on `amd64` and `arm64`
- checksums
- container images
- installer script consumption of those archives
- release manifest entries for updater selection

Required release properties:

- every tagged release has a matching `CHANGELOG.md` entry or `Unreleased` notes
  ready to be cut into that release
- every release archive includes `README`, `CHANGELOG`, `SECURITY`, and
  `LICENSE`
- every release manifest asset should include `sha256` when practical
- stable releases should be installable through the documented installer flow

## Compatibility Axes

### Product release version

This is the version a user installs. It describes the shipped build, not every
protocol boundary inside the system.

### Runtime HTTP API version

The current public Runtime HTTP surface is versioned by documentation contract,
currently `runtime-v1`:

- authoritative spec: [`docs/openapi/runtime-v1.yaml`](./docs/openapi/runtime-v1.yaml)
- breaking API changes should produce a new versioned contract, such as
  `runtime-v2`

Within a given `runtime-v1` line:

- additive fields and endpoints are allowed
- removals or incompatible payload changes require an explicit breaking-change
  note and a new versioned API contract

### Config compatibility

Config should evolve conservatively:

- additive keys are preferred
- renames should keep a compatibility window when practical
- silent meaning changes for existing keys are discouraged
- if a config key is removed or behavior changes materially, call it out in
  `CHANGELOG.md` and the example config

### Capability host contracts

Host-backed surfaces such as browser and desktop should carry versioned protocol
names in the contract itself, for example:

- `browser.v1`
- `desktop.v1`

Breaking host-wire changes should move to a new versioned contract instead of
reusing the same one.

### Skills and extension compatibility

`SKILL.md` and ClawHub compatibility are important product surfaces, but the
compatibility promise is narrower than the core Runtime HTTP API:

- keep existing `SKILL.md` loading behavior stable when practical
- document meaningful compatibility changes in docs and changelog
- avoid retroactively redefining existing metadata semantics without a migration
  note

### Unstable Future Surfaces

Future plugin protocols and generated SDKs are not part of the current release
contract unless they are implemented and documented in the trimmed release-grade
docs set.

## Support Window

The practical support policy for this repository is intentionally narrow:

- latest tagged stable release: supported
- current `main` branch before the next release: best effort
- current beta release: best effort
- nightly releases: unsupported except for maintainer triage
- older tagged releases: no compatibility or patch guarantee

This matches the current security policy in [`SECURITY.md`](./SECURITY.md).

## Deprecation Rules

When deprecating a public behavior:

1. mark it in docs or comments as deprecated
2. add a changelog note before removal when practical
3. provide the replacement path
4. keep the old behavior for at least one stable release when the migration
   burden is non-trivial

For internal-only refactors, no deprecation ceremony is required.

## Release Workflow

Recommended release flow:

1. land tested changes on `main`
2. update `CHANGELOG.md`, docs, and config examples
3. run `go test ./...`
4. cut a tag using the version rules above
5. let CI publish binaries, archives, checksums, and containers
6. publish or update the release manifest
7. smoke-test:
   - installer script
   - `hopclaw version`
   - `hopclaw update --check`
   - startup with `config.example.yaml`-derived config

## Rules For Contributors

If your pull request changes a public contract, include:

- tests or a clear explanation of why tests do not apply
- docs updates
- a changelog note when user-visible behavior changed
- a compatibility note when the change affects API, config, update behavior, or
  extension authors

If your change is roadmap-only, do not describe it as part of the current
release.
