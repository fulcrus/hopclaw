# Publishing Skills

## TL;DR

- HopClaw already supports ClawHub-style catalogs, local indexes, cached bundles, install locks, and publish flows.
- Treat local installability as the gate before you publish anything.
- Publish only versioned bundles that can still be installed with `hopclaw skills install <id> --version <version>`.

English is canonical in this file. 中文同步 follows after the English section.

## What “Publishing” Means Today

In the current codebase, skill publication is built around the ClawHub client and its filesystem-compatible layout:

- local catalog index
- cached bundles
- installed skill locks
- optional remote hub base URL and bearer auth

For operators, the day-to-day surface remains:

```bash
hopclaw skills search <query>
hopclaw skills info <id>
hopclaw skills install <id>
hopclaw skills remove <id>
```

## Safe Pre-Publish Checklist

Before you publish a skill bundle:

```bash
hopclaw skills list
hopclaw skills info <skill-id>
hopclaw skills install /path/to/skill
hopclaw message send --session-key skill-publish-check "Use <skill-id> and explain the first step."
```

You want to prove:

- discovery works
- install works from a bundle or local path
- the runtime can actually use the skill
- prerequisites are documented in `SKILL.md`

## Versioning Rule

Publish versioned artifacts. Do not mutate a bundle in place and keep the same version string.

Once a version is discoverable by catalog clients, it should stay reproducible.

## Suggested Bundle Contract

At minimum, make sure the published bundle contains:

- `SKILL.md`
- any referenced scripts or assets
- stable relative paths
- no machine-local absolute paths

## Operator Verification After Publish

```bash
hopclaw skills search <skill-id>
hopclaw skills install <skill-id> --version <version>
hopclaw skills info <skill-id>
```

If your hub or catalog is remote-backed, sync or refresh the local index before retrying installs.

## 中文同步

### TL;DR

- 当前仓库已经有 ClawHub 风格 catalog / cache / install lock / publish 流程
- 发布前先确保本地 bundle 可以安装、发现、运行
- 已发布版本应保持可复现，不要原地改包但不改版本号

### 发布前校验

```bash
hopclaw skills install /path/to/skill
hopclaw skills search <skill-id>
hopclaw skills install <skill-id> --version <version>
```

### 原则

- 先验证 installability，再谈发布
- 发布物必须是 versioned bundle
- `SKILL.md` 里引用的脚本和资源都要跟包一起走
