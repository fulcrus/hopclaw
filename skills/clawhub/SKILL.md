---
name: clawhub
description: Browse, install, and publish skills from the ClawHub skill registry
user-invocable: true
command-dispatch: tool
command-tool: clawhub.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: meta.clawhub
    emoji: "\U0001F310"
    requires:
      bins:
        - hopclaw
    always: false
---
# ClawHub

Browse, install, and publish skills from the ClawHub skill registry.

## Capabilities

- Browse available skills in the ClawHub registry
- Install skills from the registry
- Publish new skills to the registry
- Update installed skills to latest versions
- Search skills by keyword, category, or author
- View skill details and reviews

## Usage

### Browsing Skills

```bash
# List all available skills
hopclaw skill list

# Search for skills by keyword
hopclaw skill search "email"

# Search by category
hopclaw skill search --category comm

# Get skill details
hopclaw skill info skill-name
```

### Installing Skills

```bash
# Install a skill
hopclaw skill install skill-name

# Install a specific version
hopclaw skill install skill-name@1.2.0

# Install from a git URL
hopclaw skill install https://github.com/user/my-skill.git

# List installed skills
hopclaw skill installed
```

### Updating Skills

```bash
# Update a specific skill
hopclaw skill update skill-name

# Update all installed skills
hopclaw skill update --all

# Check for available updates
hopclaw skill outdated
```

### Publishing Skills

```bash
# Validate a skill before publishing
hopclaw skill validate ./my-skill/

# Publish a skill
hopclaw skill publish ./my-skill/

# Publish with a version tag
hopclaw skill publish ./my-skill/ --version 1.0.0
```

### Managing Skills

```bash
# Uninstall a skill
hopclaw skill uninstall skill-name

# Pin a skill to current version (skip updates)
hopclaw skill pin skill-name

# Unpin a skill
hopclaw skill unpin skill-name
```

## Examples

- `hopclaw skill list`
- `hopclaw skill search "github"`
- `hopclaw skill install my-skill`
- `hopclaw skill info my-skill`
- `hopclaw skill update --all`

## Error Handling

- If `hopclaw` is not installed, suggest installation instructions.
- If the registry is unreachable, check network connectivity.
- Version conflicts are reported during install. Use `--force` to override.
- Publishing requires authentication. Run `hopclaw auth login` first.
