# Security Policy

## Supported Versions

HopClaw is an actively moving runtime. Security fixes are prioritized for:

| Version line | Supported |
| --- | --- |
| Latest tagged release | Yes |
| `main` branch before the next release | Best effort |
| Older tagged releases | No |

If you depend on HopClaw in production, stay close to the latest tagged release.

## Reporting a Vulnerability

Please do not open a public GitHub issue for a security-sensitive report.

Preferred reporting channels:

1. GitHub Security Advisories private reporting:
   `https://github.com/fulcrus/hopclaw/security/advisories/new`
2. If private reporting is unavailable, contact the maintainers through the repository security policy page:
   `https://github.com/fulcrus/hopclaw/security/policy`

Include as much of the following as possible:

- affected version, commit, or deployment image tag
- attack preconditions and expected impact
- minimal reproduction steps or proof of concept
- whether the issue affects runtime, gateway, channels, helpers, update flow, or release artifacts
- whether secrets, approvals, or persistence boundaries are involved

For local operator reports, attach a redacted bundle from `hopclaw bug-report` when it helps reproduce the issue.

## Response Expectations

- Initial acknowledgment: within 5 business days
- Triage and severity assessment: within 10 business days when reproduction is possible
- Fix target: as fast as practical based on severity and release risk

We may ask for additional reproduction detail before confirming severity or impact.

## Disclosure Process

- We will validate the report, assess impact, and decide whether the issue is exploitable in supported configurations.
- Fixes may land on `main` first and then roll into the next tagged release.
- When appropriate, the release notes and `CHANGELOG.md` will call out the security fix.
- We prefer coordinated disclosure. Please give maintainers reasonable time to ship a fix before public disclosure.

## Scope

Security reports are especially helpful for issues involving:

- authentication, authorization, RBAC, or approval bypass
- unsafe tool execution, blocked-command bypass, or sandbox escape
- SSRF, local-network reachability, or unsafe external fetch paths
- secret exposure in config, logs, artifacts, or update flows
- release artifact tampering, checksum verification, or self-update trust issues
- cross-tenant, cross-workspace, or cross-session data exposure

General bugs, feature gaps, and documentation problems should still go through the normal issue tracker.
