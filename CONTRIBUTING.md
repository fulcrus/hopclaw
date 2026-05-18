# Contributing to HopClaw

Thanks for contributing to HopClaw.

This repository is a Go-based agent runtime with a relatively wide surface area: runtime orchestration, tools, channels, browser/desktop helpers, gateway APIs, and web UI assets. Please keep changes focused and verifiable.

## Before You Start

- Read [README.md](./README.md) first for the current shipped surface.
- Read [docs/README.md](./docs/README.md) for the current trimmed docs index.
- Read [VERSIONING.md](./VERSIONING.md) if your change affects release behavior, config compatibility, or public APIs.
- Search for existing issues and pull requests before starting a larger change.
- If you plan to change public behavior, include docs and tests in the same pull request.

## Helpful Contribution Areas

Concrete areas where contributions land most cleanly:

- validating installer, onboarding, and first-run docs on real machines
- improving diagnostics, `hopclaw doctor`, and `hopclaw bug-report` workflows
- reproducible runtime, gateway, channel, and capability bug reports with exact versions
- workflow gaps that fit existing extension points (skills, hooks, webhooks, channels)
- release, rollout, and operational documentation

## Development Environment

- Go `1.26.1`
- Optional:
  - Docker for image-related work
  - Node/npm for `sites/` or web UI asset work

Typical setup:

```sh
cp config.example.yaml local.yaml
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4.1-mini
```

## Useful Commands

Format:

```sh
gofmt -w ./...
```

Vet:

```sh
go vet ./...
```

Test:

```sh
go test ./...
```

Repo hygiene:

```sh
make check-repo-hygiene
```

Optional pre-commit hook:

```sh
git config core.hooksPath .githooks
```

Build:

```sh
make build
```

Run locally:

```sh
make run CONFIG=./local.yaml
```

Generate a local support bundle before filing a bug:

```sh
hopclaw bug-report
```

## Pull Request Scope

Prefer pull requests that do one thing well:

- one feature
- one refactor
- one bug fix
- one docs or release-process improvement

Avoid mixing unrelated cleanup into the same PR.

## Picking The Right Contribution Lane

Before adding code to the core repo, check whether the feature is better shipped as:

- a `SKILL.md` skill
- a webhook integration
- a stdio channel plugin
- a host-backed capability
- a built-in Go contribution

Choose the heavier lane only when the lighter one is clearly insufficient.

## Coding Expectations

- Keep changes consistent with the current repository structure and naming.
- Add or update tests for behavior changes whenever practical.
- Keep docs in sync with shipped behavior.
- Keep documentation focused on shipped behavior and contributor workflow.
- Prefer explicit, boring runtime behavior over prompt-only correctness.
- Keep the repository root release-grade. Local captures, downloaded feeds, and one-off scripts belong under `.tmp/` or another intentional subdirectory, not at top level.

## Contribution License

Unless you explicitly state otherwise, any intentionally submitted contribution
is provided under the repository's MIT License and may be redistributed
with the project's `NOTICE` file.

## Commit Messages

The existing history already follows a lightweight prefix style. Please keep using short, descriptive subjects such as:

- `feat: add channel-aware update manifest support`
- `fix: surface update availability in doctor`
- `docs: add contribution and changelog files`
- `refactor: simplify release selection logic`

## Tests Required Before Opening a PR

At minimum, run:

```sh
make check-repo-hygiene
gofmt -w ./...
go vet ./...
go test ./...
```

If your change only affects a narrow package, include the focused command you used in the PR description as well.

## Docs Required

Update docs when you change:

- CLI commands
- config keys
- HTTP endpoints
- release/update behavior
- operator workflows
- extension author workflow or compatibility expectations

Relevant files often include:

- [README.md](./README.md)
- [README.zh-CN.md](./README.zh-CN.md)
- [VERSIONING.md](./VERSIONING.md)
- [config.example.yaml](./config.example.yaml)
- files under [docs/](./docs)

## Reporting Security Issues

Please do not open public issues for suspected security vulnerabilities. Follow the process in [`SECURITY.md`](./SECURITY.md).
