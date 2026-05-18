# Bundled Extensions

This directory contains first-party extension manifests that ship with the
HopClaw workspace. They are designed to be additive: you can discover them,
copy them, or use them as templates for your own extension packs.

## What's here

- `providers/`: ready-to-customize provider bundles for common model vendors
- `tools/`: bundled skill-oriented extensions that package a plugin manifest
  alongside a `SKILL.md`

## How to enable them

When the workspace root is used as the builtins root, HopClaw can discover
plugin manifests from `extensions/` and bundled skills from the tool extension
folders. You can also copy any extension directory into a user plugin root such
as `.hopclaw/plugins/` or `.hopclaw/extensions/`.

## Build your own

Use any folder here as a starting point:

1. Copy one extension directory
2. Update `hopclaw.plugin.yaml`
3. Add or adjust `SKILL.md`, hooks, or local assets
4. Validate the manifest with `hopclaw plugins validate <path>`

## Learn more

- Plugin SDK guide: `docs/development/plugin-sdk.md`
