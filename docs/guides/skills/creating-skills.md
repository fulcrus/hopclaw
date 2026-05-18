# Creating Skills

## TL;DR

- A skill is a directory rooted by `SKILL.md`.
- Start with a local directory, validate discovery with `hopclaw skills list`, then bind the skill in a real conversation.
- Keep skills small, explicit about prerequisites, and executable with copy-pasteable commands.

English is canonical in this file. 中文同步 follows after the English section.

## Smallest Skill Layout

```text
my-skill/
└── SKILL.md
```

Minimal `SKILL.md`:

```md
# My Skill

Use this skill when the user asks for release-note triage.

## Steps

1. Read the changed files.
2. Group changes by user-visible behavior.
3. Draft a concise release note.
```

## Local Authoring Flow

Create a local skill bundle:

```bash
mkdir -p /tmp/my-skill
cat >/tmp/my-skill/SKILL.md <<'EOF'
# My Skill

Use this skill when the user asks for release-note triage.

## Steps

1. Read the changed files.
2. Group changes by user-visible behavior.
3. Draft a concise release note.
EOF
```

Then verify that HopClaw can discover it from your configured skill roots:

```bash
hopclaw skills list
hopclaw skills info my-skill
```

## What Makes A Good Skill

- State exactly when the skill should be used.
- Name required binaries, env vars, or files explicitly.
- Prefer operational steps over long narrative explanation.
- Keep commands copy-pasteable.
- Do not hide side effects.

## Real Validation

The fastest real check is a conversation that forces the runtime to inspect the skill catalog:

```bash
hopclaw message send --session-key skills-dev "Use the local skill for release-note triage on this repository."
```

Then inspect what the runtime saw:

```bash
hopclaw skills list
hopclaw tools list --json
```

## Related Surfaces

- Use [`plugin-sdk.md`](../../development/plugin-sdk.md) when you need typed Go extension points.
- Use [`using-skills.md`](./using-skills.md) when you only need to install, remove, or inspect skills.
- Use [`publishing.md`](./publishing.md) when you want to ship a skill through ClawHub-style catalogs.

## 中文同步

### TL;DR

- Skill 的最小单位就是一个带 `SKILL.md` 的目录
- 先本地写，再用 `hopclaw skills list` 验证发现是否正常
- Skill 要把触发条件、依赖和步骤写清楚

### 最小结构

```text
my-skill/
└── SKILL.md
```

### 校验方式

```bash
hopclaw skills list
hopclaw skills info my-skill
hopclaw message send --session-key skills-dev "Use the local skill for release-note triage on this repository."
```
