# HopClaw Enterprise Deployment Pack

This directory is the official low-friction deployment pack for platform teams.

It keeps HopClaw itself focused on runtime duties while packaging the pieces
that enterprise operators usually need on day one:

- `docker-compose.enterprise.yml`
  - HopClaw plus an optional bundled Caddy edge
- `Caddyfile`
  - automatic HTTPS for public-domain deployments
- `config.enterprise.yaml.example`
  - production-oriented config example with SQLite-backed durable audit delivery
- `.env.example`
  - environment variables referenced by the compose file and sample config
- `prometheus-rules.yml`
  - starter alert rules using shipped HopClaw metrics

## Supported topology

- One active HopClaw runtime per state directory
- SQLite-backed state and audit delivery durability
- Optional bundled Caddy for HTTPS termination
- External LB / ingress is still the recommended pattern for company-standard deployments

## Not claimed

- active-active multi-writer clustering against one shared state directory
- shared SQLite over NFS as a supported HA design
- built-in enterprise business logic such as tenant isolation, org model, or RBAC workflows

## Operator docs

- `../../docs/runbooks/backup-restore-cn.md`
- `../../docs/runbooks/upgrade-rollback-cn.md`
- `../../docs/runbooks/disaster-recovery-cn.md`
- `../../docs/runbooks/audit-delivery-cn.md`
- `../../docs/runbooks/common-failures-cn.md`
