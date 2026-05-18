# Testing Guide

## TL;DR

- Run focused tests while iterating, then run broader repo checks before you open a PR.
- The current repo standard centers on `go test ./...`, `go vet ./...`, and the `Makefile` quality targets.
- If your change only affects a narrow package, record the exact focused test command you used.

English is canonical in this file. 中文同步 follows after the English section.

## Fast Local Loop

Format:

```bash
gofmt -w ./...
```

Focused package test:

```bash
go test ./internal/cli
go test ./gateway
go test ./toolruntime
```

Whole repository:

```bash
go test ./...
```

## Makefile Targets

The current `Makefile` exposes the standard verification path:

```bash
make vet
make staticcheck
make staticcheck-u1000
make test
make test-race
make test-cover
make build
```

For the full gate:

```bash
make check
```

## Coverage And Benchmarks

```bash
make test-cover
make test-cover-gates
make bench
```

## Good Practice

- run the narrowest useful package test first
- keep docs and tests in the same PR when behavior changes
- do not rely on manual UI checking as the only validation
- if you touched runtime contracts, add or update at least one real-path test

## 中文同步

### TL;DR

- 开发时先跑聚焦包测试，提 PR 前再跑更大的 repo 级检查
- 当前标准命令是 `go test ./...`、`go vet ./...` 和 `Makefile` 里的质量门
- 如果只改了很窄的模块，最好在 PR 描述里写明你跑了哪个聚焦测试命令

### 常用命令

```bash
gofmt -w ./...
go test ./internal/cli
go test ./gateway
go test ./toolruntime
make check
```
