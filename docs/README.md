# Documentation

Start with the root [`README.md`](../README.md) for installation and a first
run, then use the pages below for concrete operator and developer tasks.

## Getting Started

- [`getting-started/installation.md`](./getting-started/installation.md): release installer, Homebrew, source build, Docker, and verification flow
- [`getting-started/quickstart.md`](./getting-started/quickstart.md): web-first and manual five-minute startup paths
- [`getting-started/first-conversation.md`](./getting-started/first-conversation.md): first prompt via `hopclaw message send` plus run/session inspection
- [`getting-started/configuration.md`](./getting-started/configuration.md): config locations, safe editing flow, and validation commands

## Guides

- [`guides/channels/`](./guides/channels/): per-channel setup and validation guides
- [`guides/providers/`](./guides/providers/): OpenAI, Anthropic, DeepSeek, local Ollama
- [`guides/skills/`](./guides/skills/): discover, install, author, and publish skills
- [`guides/automation/`](./guides/automation/): cron jobs, watch mode, and webhooks
- [`guides/advanced/`](./guides/advanced/): browser, desktop, knowledge base, and multi-session topics

## Reference

- [`reference/cli.md`](./reference/cli.md): core CLI reference and high-frequency commands
- [`reference/api.md`](./reference/api.md): HTTP API entry points and curl smoke tests
- [`reference/config-reference.md`](./reference/config-reference.md): full configuration schema
- [`reference/slash-commands.md`](./reference/slash-commands.md): REPL slash commands
- [`reference/tool-reference.md`](./reference/tool-reference.md): built-in tool families
- [`reference/acp-protocol.md`](./reference/acp-protocol.md): ACP transport, methods, and session flow
- [`reference/gateway-operator-websocket.md`](./reference/gateway-operator-websocket.md): operator WebSocket contract
- [`openapi/runtime-v1.yaml`](./openapi/runtime-v1.yaml): OpenAPI 3.1 description of the Runtime API
- [`openapi/README.md`](./openapi/README.md): local Swagger UI and Redoc preview instructions

## Troubleshooting

- [`troubleshooting/doctor.md`](./troubleshooting/doctor.md): `hopclaw doctor` sections, `--fix`, and operator workflow
- [`troubleshooting/common-issues.md`](./troubleshooting/common-issues.md): common startup, auth, model, and skill failures
- [`troubleshooting/faq.md`](./troubleshooting/faq.md): short answers for first-run and operator questions

## Operations

- [`enterprise-webhook-quickstart.md`](./enterprise-webhook-quickstart.md): no-core-patch quickstart for external AuthZ and audit fan-out
- [`runbooks/backup-restore-cn.md`](./runbooks/backup-restore-cn.md)
- [`runbooks/upgrade-rollback-cn.md`](./runbooks/upgrade-rollback-cn.md)
- [`runbooks/disaster-recovery-cn.md`](./runbooks/disaster-recovery-cn.md)
- [`runbooks/audit-delivery-cn.md`](./runbooks/audit-delivery-cn.md)
- [`runbooks/common-failures-cn.md`](./runbooks/common-failures-cn.md)

## Development

- [`development/contributing.md`](./development/contributing.md): release-grade contribution workflow
- [`development/testing.md`](./development/testing.md): focused package testing plus repo-wide verification
- [`development/plugin-sdk.md`](./development/plugin-sdk.md): typed Go SDK for tool, channel, provider, and skill plugins

## Project Policies

- [`../SECURITY.md`](../SECURITY.md): private vulnerability reporting process
- [`../VERSIONING.md`](../VERSIONING.md): release channels, compatibility promises, and deprecation rules
- [`../CHANGELOG.md`](../CHANGELOG.md): versioned changes
