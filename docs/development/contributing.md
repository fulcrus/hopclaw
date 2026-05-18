# Contributing Guide

## TL;DR

- Use Go `1.26.1`.
- Keep pull requests focused and verifiable.
- Before opening a PR, run `make check-repo-hygiene`, `gofmt -w ./...`, `go vet ./...`, and `go test ./...`.
- If you change user-visible behavior, update docs in the same PR.

English is canonical in this file. 中文同步 follows after the English section.

## Before You Start

Read these files first:

- `README.md`
- `docs/README.md`
- `VERSIONING.md`

## High-Leverage Contribution Lanes

The repository surface is broad. These contribution lanes usually create the best signal quickly:

- installer, onboarding, and first-run fixes
- diagnostics improvements around `hopclaw doctor` and `hopclaw bug-report`
- reproducible runtime, gateway, channel, and capability bugs
- docs, examples, and operator workflow gaps
- extension surfaces that fit existing release-grade patterns

## Choose The Lightest Integration Path First

Not every feature belongs in the core runtime.

Use this heuristic:

| Goal | Start with |
| --- | --- |
| Package prompts, workflow rules, or helper scripts | a `SKILL.md` skill |
| React to lifecycle events or send alerts | a hook |
| Integrate an external system through HTTP | a webhook-based integration |
| Add a process-isolated messaging bridge | a stdio channel plugin |
| Add a core runtime capability or provider | a Go contribution in this repo |

## Development Environment

Required:

- Go `1.26.1`

Useful optional dependencies:

- Docker for sandbox or image-related work
- Node/npm for web UI or site work

Typical local setup:

```bash
cp config.example.yaml local.yaml
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4.1-mini
```

## Common Commands

### Format

```bash
gofmt -w ./...
```

### Static checks

```bash
go vet ./...
make check-repo-hygiene
```

### Tests

```bash
go test ./...
```

### Build

```bash
make build
```

### Run locally

```bash
make run CONFIG=./local.yaml
```

### Generate a support bundle

```bash
hopclaw bug-report
```

## Optional Git Hook

Enable the repository hook set:

```bash
git config core.hooksPath .githooks
```

The repo currently ships:

```bash
ls -la .githooks
```

## Minimum Checks Before Opening A PR

Run these commands unless your change is completely documentation-only:

```bash
make check-repo-hygiene
gofmt -w ./...
go vet ./...
go test ./...
```

If your change is narrow, also include focused package tests in the PR description, for example:

```bash
go test ./channels/slack
go test ./internal/cli
go test ./sdk/plugin/...
```

## Pull Request Scope

Prefer pull requests that do one thing well:

- one feature
- one bug fix
- one refactor
- one docs improvement
- one release-process or tooling change

Avoid mixing unrelated cleanup into the same PR.

## Coding Expectations

- Keep changes consistent with the existing package layout and naming.
- Add or update tests for behavior changes whenever practical.
- Keep docs in sync with shipped behavior.
- Prefer explicit, boring runtime behavior over prompt-only cleverness.
- Do not treat the repository root as a scratch space; use `docs/`, `scripts/`, or `.tmp/` intentionally.

## Documentation Expectations

Update docs when you change:

- CLI commands
- config keys
- HTTP endpoints
- runtime or operator workflows
- release/update behavior
- extension author workflow

Files commonly touched together:

- `README.md`
- `README.zh-CN.md`
- `VERSIONING.md`
- `config.example.yaml`
- files under `docs/`

## Commit Messages

The repository already uses a lightweight prefix style. Follow it:

```text
feat: add channel-aware update manifest support
fix: surface update availability in doctor
docs: add contribution and changelog files
refactor: simplify release selection logic
```

## Security Reporting

Do not file public issues for suspected vulnerabilities.

Use the private security reporting process documented by the project. If the GitHub security advisory workflow is enabled, use that instead of public issues.

## Practical Contribution Flow

```bash
git checkout -b fix/my-change
gofmt -w ./...
go vet ./...
go test ./...
git status
```

If the change affects operator behavior, validate it with the CLI too:

```bash
hopclaw config validate
hopclaw doctor
hopclaw status
```

## When Your Change Is Better As An External Extension

If you can ship the idea as one of these, prefer that first:

- a skill
- a webhook integration
- a stdio channel plugin
- a host-backed capability

That keeps the core repository smaller and preserves a clearer release boundary.

## 中文同步

### TL;DR

- 使用 Go `1.26.1`。
- PR 尽量单一、可验证。
- 开 PR 前至少跑：`make check-repo-hygiene`、`gofmt -w ./...`、`go vet ./...`、`go test ./...`。
- 改了用户可见行为，就在同一个 PR 里把文档一起更新。

### 常用命令

```bash
gofmt -w ./...
go vet ./...
go test ./...
make build
make run CONFIG=./local.yaml
git config core.hooksPath .githooks
```

