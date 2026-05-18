# Skill Template

This template is the lightest-weight way to extend HopClaw.

Use it when:

- the runtime already has the underlying tools you need
- you want to package domain instructions, checklists, and safe usage rules
- you may also want a small local helper script that the agent can invoke via
  builtin execution tools

## Files

- `SKILL.md`: the skill contract loaded by HopClaw
- `scripts/run.sh`: shell helper example
- `scripts/run.py`: Python helper example
- `scripts/run.mjs`: Node.js helper example

## Quick Start

1. Copy this folder into your skill directory, for example:

```sh
cp -R examples/skill-template "$HOME/.hopclaw/skills/my-skill"
```

2. Rename the skill in `SKILL.md`.

3. Replace the example task guidance with your real workflow.

4. If you keep the helper script, make it executable:

```sh
chmod +x "$HOME/.hopclaw/skills/my-skill/scripts/run.sh"
```

5. Point the skill instructions at your real commands, APIs, and validation
   steps.

You can choose whichever helper runtime fits your users:

```sh
./scripts/run.sh "<primary-input>"
python3 ./scripts/run.py "<primary-input>"
node ./scripts/run.mjs "<primary-input>"
```

## What To Customize

- `name`, `description`, and `metadata.openclaw.skillKey`
- required binaries or environment variables under `metadata.openclaw.requires`
- the "When to use" and "Workflow" sections
- the helper script input and output format
- security and approval notes

## Design Notes

- `SKILL.md` is a compatibility and instruction layer, not a compiled tool
  implementation by itself.
- Keep the instructions explicit and operational. Tell the agent which builtin
  tools to use and in what order.
- Prefer deterministic helper scripts over long inline shell fragments when the
  workflow is repetitive.
