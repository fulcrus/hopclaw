# Doctor

## TL;DR

- `hopclaw doctor` is the fastest operator check when startup, auth, connectivity, storage, or helper readiness feels wrong.
- The command is sectioned: `auth`, `config`, `connectivity`, `skills`, `storage`, `security`, and `platform`.
- Use `--fix` only after you understand what the command is going to remediate.

English is canonical in this file. 中文同步 follows after the English section.

## Main Commands

Run all checks:

```bash
hopclaw doctor
```

Run one section:

```bash
hopclaw doctor auth
hopclaw doctor config
hopclaw doctor connectivity
hopclaw doctor skills
hopclaw doctor storage
hopclaw doctor security
hopclaw doctor platform
```

Attempt safe remediations:

```bash
hopclaw doctor --fix
```

## What Each Section Covers

- `auth`
  - operator auth configuration
  - provider credentials in env or config
  - production-profile auth safety
- `config`
  - config file presence and syntax
  - migration and model configuration
- `connectivity`
  - gateway reachability
  - gateway health
  - provider connectivity
  - channel health and webhooks
- `skills`
  - skill roots and runtime dependencies
- `storage`
  - state directory
  - runtime, control, knowledge, and audit databases
  - stale session locks
  - disk space
- `security`
  - plaintext secret patterns
  - config permission hygiene
- `platform`
  - runtime compatibility
  - daemon/helper expectations

## Typical Operator Flow

```bash
hopclaw doctor
hopclaw doctor connectivity
hopclaw doctor storage
hopclaw channels status
hopclaw models list
```

## JSON Output

Use JSON when you want to feed results into another script:

```bash
hopclaw doctor --json
hopclaw doctor connectivity --json
```

## 中文同步

### TL;DR

- 启动、认证、连通性、存储、helper 状态异常时，先跑 `hopclaw doctor`
- 当前子项包括 `auth`、`config`、`connectivity`、`skills`、`storage`、`security`、`platform`
- `--fix` 适合在你已经理解问题后再用

### 常用命令

```bash
hopclaw doctor
hopclaw doctor connectivity
hopclaw doctor storage
hopclaw doctor --fix
```
