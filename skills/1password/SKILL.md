---
name: 1password
description: Manage secrets, vaults, and credentials via the 1Password CLI
homepage: https://developer.1password.com/docs/cli
user-invocable: true
command-dispatch: tool
command-tool: 1password.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: sec.1password
    emoji: "\U0001F510"
    primaryEnv: OP_SERVICE_ACCOUNT_TOKEN
    requires:
      bins:
        - op
      env:
        - OP_SERVICE_ACCOUNT_TOKEN
    always: false
---
# 1Password

Manage secrets, vaults, and credentials using the 1Password CLI (`op`).

## Capabilities

- Read and write secrets in 1Password vaults
- Manage vaults (list, create, delete)
- Generate strong passwords with configurable options
- Share items securely via 1Password sharing links
- List and search items across vaults
- Inject secrets into environment variables or config files

## Authentication

Requires a 1Password Service Account Token:

- `OP_SERVICE_ACCOUNT_TOKEN`: Service account token for CLI authentication

## Usage

Use the `op` CLI to interact with 1Password. Always confirm vault and item names before making changes.

### Reading Secrets

```bash
# List all vaults
op vault list --format=json

# List items in a vault
op item list --vault "Private" --format=json

# Get a specific item
op item get "Database Credentials" --vault "Private" --format=json

# Read a specific field
op item get "Database Credentials" --fields label=password
```

### Writing Secrets

```bash
# Create a new login item
op item create --category=login \
  --title="New Service" \
  --vault="Private" \
  --url="https://example.com" \
  username=admin password=secret123

# Edit an existing item
op item edit "New Service" --vault="Private" password=newpassword
```

### Generating Passwords

```bash
# Generate a random password (default settings)
op item create --generate-password --category=password --title="Generated"

# Generate with specific requirements
op item create --generate-password=20,letters,digits,symbols --category=password --title="Strong"
```

## Examples

- `op vault list --format=json`
- `op item list --vault "Private" --format=json`
- `op item get "API Key" --fields label=credential`
- `op item create --category=login --title="Service" username=admin password=secret`

## Security

- Never display secret values in plain text unless the user explicitly requests it.
- Prefer using `op://` secret references over raw values when possible.
- Always use `--format=json` for machine-readable output.
- Do not cache or store retrieved secrets in temporary files.
