---
name: healthcheck
description: Check URL reachability, latency, redirects, and certificate state using existing network capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: ops.healthcheck
    emoji: "\U0001F3E5"
    always: false
---
# Health Check

Use current network capabilities to verify whether a URL or endpoint is healthy.

Preferred approach:

- Use `net.fetch` or the current HTTP capability to inspect status code, headers, latency, and redirect behavior.
- Use browser capabilities only when the health question depends on rendered UI behavior rather than raw HTTP reachability.
- Treat health-check results as point-in-time observations and report when the check was performed.
- If the current tool list lacks the needed capability, use `skill.ensure` before reaching for shell probes.

Working rules:

- Include the checked URL, timestamp, final URL after redirects, HTTP status, and the primary failure reason when the check fails.
- Surface TLS or certificate issues when the available capability exposes them.
- Distinguish network failure, TLS failure, auth failure, rate limiting, and application error instead of collapsing everything into "down".
- Do not default to `curl`, `openssl`, or manual shell probes unless the user explicitly asked for that path.
