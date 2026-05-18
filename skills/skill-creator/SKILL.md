---
name: skill-creator
description: Guide users through creating new SKILL.md files with proper format and validation
user-invocable: true
command-dispatch: tool
command-tool: skill-creator.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: meta.skill-creator
    emoji: "\U0001F9F0"
    always: false
---
# Skill Creator

Guide users through creating new SKILL.md files with proper format and validation.

## Capabilities

- Walk through creating a new skill step by step
- Validate SKILL.md format and required fields
- Generate SKILL.md from a description of capabilities
- Suggest appropriate skillKey, emoji, and metadata
- Check for common mistakes and missing fields

## Usage

This skill provides LLM-guided instructions for creating new skills. No external tools required.

### Required SKILL.md Fields

Every SKILL.md must include:

1. **YAML frontmatter** between `---` markers
2. **name**: Unique skill identifier (lowercase, hyphens allowed)
3. **description**: One-line description of the skill
4. **metadata.openclaw.skillKey**: Dot-notation category key (e.g., `dev.github`)

### Optional but Recommended Fields

- **homepage**: URL to documentation or project page
- **user-invocable**: Whether users can invoke directly (default: true)
- **command-dispatch**: How commands are dispatched (`tool`)
- **command-tool**: Tool name for dispatch (`{name}.run`)
- **command-arg-mode**: Argument passing mode (`raw`)
- **metadata.openclaw.emoji**: Display emoji
- **metadata.openclaw.primaryEnv**: Primary environment variable
- **metadata.openclaw.requires**: Binary and environment requirements

### Skill Categories (skillKey prefixes)

- `dev.*` - Development tools (git, github, docker)
- `comm.*` - Communication (slack, email, messaging)
- `prod.*` - Productivity (notes, todos, calendars)
- `media.*` - Media (audio, video, images)
- `ai.*` - AI services (image gen, speech, embeddings)
- `iot.*` - IoT and hardware (lights, cameras, sensors)
- `ops.*` - Operations (monitoring, deployment)
- `util.*` - Utilities (PDF, screenshot, web)
- `sec.*` - Security (secrets, auth, encryption)
- `meta.*` - Meta skills (skill management, registry)
- `audio.*` - Audio processing (TTS, recognition)

### Template

```markdown
---
name: my-skill
description: Brief description of what the skill does
homepage: https://example.com
user-invocable: true
command-dispatch: tool
command-tool: my-skill.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: category.my-skill
    emoji: "\U0001Fxxx"
    primaryEnv: MY_API_KEY
    requires:
      bins:
        - required-binary
      env:
        - MY_API_KEY
    always: false
---
# Skill Title

Description of the skill.

## Capabilities

- Capability 1
- Capability 2

## Usage

How to use the skill with examples.

## Examples

- `command example 1`
- `command example 2`
```

## Examples

- "Create a skill for interacting with the Notion API"
- "Help me write a SKILL.md for a weather service"
- "Validate my SKILL.md file"
