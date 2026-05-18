# Hook Integration Samples

This directory contains runnable sample wrappers for HopClaw hook payloads.

## Included Samples

- `send-slack-governance-alert.sh`: format governance dead-letter payloads into a Slack incoming-webhook message
- `send-feishu-governance-alert.sh`: format governance dead-letter payloads into a Feishu bot text message
- `send-governance-email-alert.sh`: render a governance alert email and send it through `sendmail` or write an `.eml` file to `HOOK_OUTBOX_DIR`
- `open-governance-incident.sh`: turn governance dead-letter payloads into a generic incident JSON document
- `escalate-governance-retry.sh`: turn governance retry payloads into a generic escalation JSON document
- `serve-sample-webhook.py`: local HTTP receiver for webhook-based templates such as `Governance Dead-Letter Webhook` or `Approval Resolved Callback Webhook`

## Local Smoke Test

Write generated payloads to a local outbox directory without calling any external
system:

```bash
mkdir -p /tmp/hopclaw-hook-outbox
cat ./scripts/hooks/examples/governance_dead_letter_payload.json | \
  HOOK_OUTBOX_DIR=/tmp/hopclaw-hook-outbox \
  ./scripts/hooks/send-slack-governance-alert.sh
```

For the email sample, set `HOOK_OUTBOX_DIR` to force `.eml` rendering into the
local outbox before trying `sendmail`:

```bash
cat ./scripts/hooks/examples/governance_dead_letter_payload.json | \
  EMAIL_TO=ops@example.com \
  HOOK_OUTBOX_DIR=/tmp/hopclaw-hook-outbox \
  ./scripts/hooks/send-governance-email-alert.sh
```

Start a local webhook receiver:

```bash
python3 ./scripts/hooks/serve-sample-webhook.py --port 8787 --outbox-dir /tmp/hopclaw-webhook-inbox
```

Then point a webhook template URL at `http://127.0.0.1:8787/governance/dead-letter`
or `http://127.0.0.1:8787/approval/resolved`.

If your local environment blocks binding a loopback TCP port, skip the webhook
receiver smoke test and validate the command templates or run the receiver in a
host environment with port binding enabled.

## Environment Variables

- `HOOK_OUTBOX_DIR`: if set, command samples write their rendered request body to this directory instead of failing when no remote endpoint is configured
- `SLACK_WEBHOOK_URL`: target URL for Slack incoming webhooks
- `FEISHU_WEBHOOK_URL`: target URL for Feishu custom bots
- `EMAIL_TO`: recipient used by the email sample
- `EMAIL_FROM`: sender used by the email sample, defaults to `hopclaw@example.local`
- `SENDMAIL_BIN`: optional explicit `sendmail` binary path for the email sample
- `INCIDENT_WEBHOOK_URL`: target URL for the generic incident sample
- `RETRY_ESCALATION_WEBHOOK_URL`: target URL for the retry escalation sample

Related references:

- [`../../examples/hook-template/README.md`](../../examples/hook-template/README.md)
- [`../../hooks/types.go`](../../hooks/types.go)
