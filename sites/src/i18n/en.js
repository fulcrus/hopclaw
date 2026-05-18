export default {
  nav: {
    runtime: "How It Works",
    features: "Capabilities",
    useCases: "Use Cases",
    docs: "Docs",
    install: "Install",
    telemetry: "Telemetry",
    skills: "Migration",
    github: "GitHub",
    tagline: "Use it yourself first, then roll it out to your team",
  },
  ui: {
    copy: "Copy",
    copied: "Copied",
  },
  footer: {
    blurb:
      "HopClaw is a Go-built agent runtime you can install for yourself first, then expand into team workflows and enterprise integrations when you are ready.",
    note: "The shipped value today is practical: REPL, dashboard, browser and desktop capability, file and exec tools, approvals, audit, APIs, and a migration bridge for existing SKILL.md assets.",
    license: "Apache-2.0 · Go-built agent runtime",
    product: "Official site",
    resources: "Source of truth",
    repo: "Build with us",
    quickstart: "Quick Start",
    cli: "CLI Reference",
    enterpriseDeploy: "Enterprise Deploy",
    telemetry: "Telemetry policy",
    readme: "README",
    readmeCN: "README (ZH)",
    docsIndex: "Docs index",
    configExample: "config.example.yaml",
    issues: "Issues",
    releases: "Releases",
    bugTemplate: "Bug template",
    featureTemplate: "Workflow request",
    contributing: "Contributing",
    copyright: "© 2026 HopClaw. Built in Go.",
  },
  layers: {
    badge: "Operating surface",
    cards: [
      {
        tone: "runtime",
        badge: "Governed core",
        title: "Execution, approvals, and audit in one runtime",
        desc: "Runs, sessions, tool dispatch, approvals, artifacts, audit, and the HTTP control plane stay together in the main Go runtime.",
        tags: ["runs", "approvals", "artifacts", "audit"],
      },
      {
        tone: "system",
        badge: "Channels / Hosts",
        title: "Chat, browser, desktop, and system-linked capability",
        desc: "Channels, Layer 2 groups, browser helpers, and desktop helpers are exposed only when the current environment is actually ready to support them.",
        tags: ["git.*", "container.*", "browser.*", "desktop.*"],
      },
      {
        tone: "skills",
        badge: "Migration bridge",
        title: "Carry existing skill assets forward",
        desc: "HopClaw can read SKILL.md packs from familiar compatibility roots, hot-refresh them, and recover missing capability through governed skill.ensure.",
        tags: ["SKILL.md", "~/.openclaw", "ClawHub bundles", "skill.ensure"],
      },
    ],
    flowProbe: "ingress -> runtime -> policy -> operator APIs",
    flowSkill: "skill roots -> ensure/install -> approval or policy",
  },
  home: {
    badge: "Warm, practical AI that starts on your own machine",
    titleLines: [
      "Install HopClaw and let AI do real work.",
      "Keep it simple now, scale it later.",
    ],
    desc: "HopClaw is a Go-built agent runtime for people who want useful AI, not more noise. Start locally with browser, desktop, files, and commands. When your team needs it, use the same core runtime for service, API, approval, and audit flows.",
    heroPoints: [
      "Use it yourself first on your own machine",
      "Open pages, handle files, run commands, and keep results visible",
      "Add team or enterprise controls only when you actually need them",
    ],
    primaryCta: "Install in 5 minutes",
    secondaryCta: "See use cases",
    installBadge: "Quick Start",
    installTitle: "Start here",
    installOptions: [
      {
        id: "unix",
        label: "macOS / Linux",
        desc: "One command installs HopClaw and opens the guided local setup.",
        command:
          "curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh",
      },
      {
        id: "windows",
        label: "Windows",
        desc: "PowerShell install with the same guided onboarding path.",
        command:
          "$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex",
      },
    ],
    installChecks: [
      {
        step: "01",
        title: "Install",
        desc: "Run the installer and let HopClaw set up the first local runtime.",
      },
      {
        step: "02",
        title: "Open HopClaw",
        desc: "Enter the interactive client and finish setup in the dashboard.",
      },
      {
        step: "03",
        title: "Try one real task",
        desc: "Use browser, file, or command workflows and verify the result.",
      },
    ],
    valueEyebrow: "Why people actually keep using it",
    valueTitle: "Built for real work, not just better demos",
    valueDesc: "The first experience should feel useful right away and still stay understandable later.",
    valueCards: [
      {
        badge: "Do work",
        title: "It can act, not only answer",
        desc: "Use one runtime for browser steps, desktop actions, files, commands, and results you can inspect later.",
      },
      {
        badge: "Stay clear",
        title: "You can see what happened",
        desc: "Dashboard, health checks, doctor, runs, and artifacts make it easier to trust what the system actually did.",
      },
      {
        badge: "Grow later",
        title: "Start alone, expand when needed",
        desc: "A personal install does not lock you out of future team rollout, self-hosting, API access, or enterprise controls.",
      },
    ],
    expandEyebrow: "From personal use to team rollout",
    expandTitle: "Use the same product in two stages",
    expandDesc: "Most users should start locally. When the value is clear, the same runtime can be expanded for teams and enterprise integration.",
    personalPath: {
      badge: "For personal use",
      title: "Start local and keep it simple",
      desc: "Best for developers, researchers, operators, writers, and power users who want AI to help with everyday computer work.",
      points: [
        "Run `hopclaw` and use the interactive client",
        "Add browser or desktop helpers only when needed",
        "Use `hopclaw doctor` when something feels off",
      ],
    },
    teamPath: {
      badge: "For teams and enterprise",
      title: "Roll it out without changing the core",
      desc: "When you need service mode, APIs, approvals, and audit, HopClaw already has a clean path for self-hosted rollout.",
      points: [
        "Use `hopclaw serve` for the service entrypoint",
        "Start from the official enterprise deploy pack",
        "Keep tenant, org, and RBAC business logic in your own systems",
      ],
    },
    enterpriseCards: [
      {
        badge: "Approvals",
        title: "Pause risky actions when required",
        desc: "Approval flows are available when you need stronger control around write or high-risk execution.",
      },
      {
        badge: "Audit",
        title: "Keep an audit trail for production use",
        desc: "Audit delivery supports reliable export so enterprise rollout is not forced into best-effort logging.",
      },
      {
        badge: "Deployment",
        title: "Use the official deploy pack as the starting point",
        desc: "Compose, sample config, HTTPS edge, alert rules, and runbooks already exist in the repository.",
      },
    ],
    ctaEyebrow: "Ready to try it",
    ctaTitle: "Install it first. Decide after one real task.",
    ctaDesc: "The fastest way to evaluate HopClaw is not another article. Install it, run one useful workflow, and see whether it earns a place in your daily setup or your team stack.",
    ctaPrimary: "Install now",
    ctaSecondary: "Open GitHub",
  },
  runtime: {
    badge: "Runtime contract",
    title: "A runtime that keeps execution visible",
    desc: "From ingress to stored result, HopClaw keeps team-facing execution legible: what started, what paused, what resumed, what produced artifacts, and which tools were actually available.",
    mapEyebrow: "Request path",
    mapTitle: "The four layers operators actually depend on",
    mapDesc:
      "Reliability comes from what sits beneath the UI: ingress, execution, governance, and optional hosts.",
    mapCards: [
      {
        title: "Ingress",
        desc: "Channels and HTTP create work, bind sessions, and enqueue runs without mixing transport-specific rules into the agent loop.",
      },
      {
        title: "Execution core",
        desc: "The runtime prepares context, routes models, dispatches tools, persists artifacts, and publishes events as one service contract.",
      },
      {
        title: "Governance",
        desc: "Approvals, audit, install policy, and runtime profiles define how risky work pauses, resumes, and gets explained.",
      },
      {
        title: "Optional hosts",
        desc: "browser.* and desktop.* stay outside the core binary until helpers are configured, authenticated, and healthy.",
      },
    ],
    bootEyebrow: "Boot contract",
    bootTitle: "Declare the real operating surface at boot",
    bootDesc:
      "A trustworthy runtime should say which profile, channels, hosts, tools, and approval semantics are active before the first run begins.",
    bootPoints: [
      "Register built-ins and the currently valid Layer 2 groups.",
      "Detect configured hosts, compatibility roots, and runtime profile.",
      "Expose tools and operator endpoints over HTTP with a concrete readiness state.",
    ],
    flowEyebrow: "Run lifecycle",
    flowTitle: "One lifecycle across chat, HTTP, and operator workflows",
    flowDesc:
      "Regardless of entry point, the runtime keeps one execution shape so state and side effects remain inspectable.",
    flowSteps: [
      {
        step: "01",
        title: "Create run",
        desc: "An inbound request is bound to a session key and persisted before execution begins.",
      },
      {
        step: "02",
        title: "Prepare context",
        desc: "The runtime compacts history, applies token budgets, and constructs the model-facing window.",
      },
      {
        step: "03",
        title: "Call tools",
        desc: "Built-ins, host-backed tools, and compatible skills execute under the same runtime contract.",
      },
      {
        step: "04",
        title: "Gate side effects",
        desc: "Risky operations pause behind approval or policy instead of letting the model define trust boundaries alone.",
      },
      {
        step: "05",
        title: "Persist output",
        desc: "Large payloads become artifacts and events so operators can inspect what happened later.",
      },
      {
        step: "06",
        title: "Resume or finish",
        desc: "Paused work resumes with an explicit decision, or the run closes with a final queryable status.",
      },
    ],
    profilesEyebrow: "Profiles",
    profilesTitle: "Profiles that change actual behavior",
    profilesDesc:
      "The shipped profiles alter defaults in concrete ways instead of hiding behind a vague “prod mode”.",
    profiles: [
      {
        name: "desktop",
        desc: "Local-first default with approval gates still present for write-like or high-risk operations.",
      },
      {
        name: "trusted_desktop",
        desc: "Lower friction on a personal machine without turning destructive behavior into a silent default.",
      },
      {
        name: "production",
        desc: "Requires auth, durable state, stricter exec defaults, and audit-enabled startup before serving traffic.",
      },
    ],
    boundaryEyebrow: "Current scope",
    boundaryTitle: "What ships today versus what stays out",
    boundaryDesc:
      "The runtime surface is already usable in real operations. Some larger platform layers are deliberately still out of scope.",
    shippedTitle: "Implemented now",
    shipped: [
      "Run and session lifecycle, approvals, artifacts, audit, and event bus.",
      "HTTP runtime API, local dashboard, and core operator workflows.",
      "Built-in tools, Layer 2 groups, and host-backed browser/desktop exposure.",
      "Migration-aware skill loading with auto-refresh and policy-driven ensure/install.",
      "Built-in channel adapters plus shipped skills and bundles for messaging, productivity, and webhook ingress.",
    ],
    notShippedTitle: "Still expanding",
    notShipped: [
      "Broader gRPC plugin host and protocol surface.",
      "Generated SDK coverage across more languages and clients.",
      "Richer browser orchestration such as downloads and deeper multi-tab flows.",
      "More complete operator console workflows for every long-running scenario.",
    ],
  },
  features: {
    badge: "Runtime surfaces",
    title: "Built for operators, not just prompt builders",
    desc: "HopClaw keeps the runtime legible once agents touch shells, files, chat systems, browsers, desktops, and live business tools.",
    pillarsEyebrow: "Core surfaces",
    pillarsTitle: "What users and operators gain immediately",
    pillarsDesc:
      "The value is not novelty. It is less ambiguity at the edge where real work happens.",
    pillars: [
      {
        title: "Governed execution",
        desc: "Approvals, audit, artifacts, and operator-visible state stay inside the runtime instead of being delegated to prompts.",
      },
      {
        title: "Visible runtime state",
        desc: "Runs, approvals, artifacts, and tool exposure are queryable instead of being trapped inside chat history.",
      },
      {
        title: "Channel and host coverage",
        desc: "Chat ingress, browser helpers, desktop helpers, and office workflows all attach to one runtime contract.",
      },
      {
        title: "Migration bridge",
        desc: "Teams can bring SKILL.md assets forward without making compatibility the main product identity.",
      },
    ],
    toolsEyebrow: "Capability groups",
    toolsTitle: "One runtime surface, several capability sources",
    toolsDesc:
      "Built-ins, Layer 2 groups, hosts, channels, and skills are exposed intentionally based on what is actually available and governable.",
    toolFamilies: [
      {
        title: "Core tools",
        desc: "File, exec, net, text, runtime, audit, and utility tools built directly into the Go binary.",
      },
      {
        title: "Layer 2 groups",
        desc: "Git, packages, containers, search, speech, media, and related system-linked capability sets.",
      },
      {
        title: "Browser host",
        desc: "navigate, click, type, wait, snapshot, screenshot, and tab lifecycle through hopclaw-browserd.",
      },
      {
        title: "Desktop host",
        desc: "window focus, tree capture, hotkeys, screenshots, and clipboard flows through hopclaw-desktopd.",
      },
      {
        title: "Channels",
        desc: "Chat and webhook adapters feed the same run lifecycle rather than creating a second-class execution path.",
      },
      {
        title: "Compatible skills",
        desc: "Local SKILL.md discovery plus policy-governed ensure/install for GitHub, Notion, Jira, Trello, Slack, email, Feishu/Lark, and related work bundles.",
      },
    ],
    securityEyebrow: "Risk controls",
    securityTitle: "Risk controls are built into the runtime",
    securityDesc: "Risk management should survive beyond the prompt window.",
    securityCards: [
      {
        title: "Approval tickets",
        desc: "Potentially risky operations create runtime approvals that can be resolved and resumed later.",
      },
      {
        title: "Audit trail",
        desc: "Tool execution, approvals, and related safety events are persisted for later inspection.",
      },
      {
        title: "Runtime profiles",
        desc: "desktop, trusted_desktop, and production change defaults before the first run begins.",
      },
      {
        title: "Artifacts over chat spam",
        desc: "Large outputs can be stored and referenced without flooding the model window or operator transcript.",
      },
    ],
    contextEyebrow: "Context control",
    contextTitle: "Context stays bounded on purpose",
    contextDesc:
      "Long-running work stays usable because the runtime compacts history, reserves output space, and trims tool payloads deliberately.",
    contextNotes: [
      "Sliding-window compaction preserves system policy, oldest anchors, and newest messages while summarizing the middle.",
      "Token budgets reserve output space instead of letting long input consume the whole model window.",
      "Large tool results are soft-trimmed and can be persisted as artifacts for later inspection.",
    ],
    contextBands: [
      "system policy",
      "oldest anchors",
      "rolling summary",
      "latest messages",
      "reserved output",
    ],
    surfaceEyebrow: "API surface",
    surfaceTitle: "The HTTP control plane is part of the product",
    surfaceDesc:
      "The dashboard is only one view. The durable operator contract is the API underneath it.",
    area: "Area",
    surface: "Endpoint / surface",
    whenToUse: "Use it for",
    surfaceRows: [
      {
        area: "Health",
        path: "GET /healthz",
        desc: "Readiness checks and smoke probes.",
      },
      {
        area: "Runs",
        path: "POST /runtime/runs",
        desc: "Create or enqueue new work from your own systems.",
      },
      {
        area: "Status",
        path: "GET /runtime/runs/:id",
        desc: "Inspect run state, result, and stored references.",
      },
      {
        area: "Approvals",
        path: "GET /runtime/approvals",
        desc: "List pending decisions and operator backlog.",
      },
      {
        area: "Artifacts",
        path: "GET /runtime/artifacts",
        desc: "Inspect large outputs without replaying the run.",
      },
      {
        area: "Tools",
        path: "GET /runtime/tools",
        desc: "Discover what the runtime can actually do right now.",
      },
    ],
  },
  useCases: {
    badge: "Where it fits",
    title: "From personal desktops to team workflows, where HopClaw fits",
    desc: "It is not only a small-team asset manager for prompts, skills, and tools, and it is not only a chat bot. You can use it as a personal desktop runtime, a team workflow execution layer, or a governed runtime wired into enterprise systems through HTTP and hooks.",
    cases: [
      {
        eyebrow: "Personal desktop",
        title: "Start on your own machine with desktop, browser, and system tools",
        desc: "For individual developers and power users, the first value is often not collaboration. It is bringing browser.*, desktop.*, files, commands, and knowledge workflows into one local runtime.",
        outcomes: [
          "Use browser.* when a web product has no practical API path.",
          "Use desktop.* for screenshots, hotkeys, focus, and clipboard work.",
          "Inspect local state through the dashboard, doctor, and artifacts.",
        ],
      },
      {
        eyebrow: "Team chat ops",
        title: "Put release, ops, and workflow assistants behind approvals",
        desc: "Use channel adapters plus policy gates when an assistant needs to deploy, inspect logs, or change infrastructure from Slack, Discord, Telegram, or Feishu.",
        outcomes: [
          "Keep one runtime surface across multiple chat products.",
          "Pause on risky tools and resume after human approval.",
          "Audit what changed instead of trusting a transcript alone.",
        ],
      },
      {
        eyebrow: "Enterprise integration",
        title: "Drive runs from internal systems, schedulers, and private platforms",
        desc: "If you already have schedulers, webhook sources, or internal platforms, HopClaw can sit behind HTTP and approval hooks instead of pretending chat is the only control plane. It handles execution; your own systems keep tenant and permission logic.",
        outcomes: [
          "Create runs asynchronously and poll them later.",
          "Inspect approvals, artifacts, tools, and run state over HTTP.",
          "Reconnect approvals, audit, and business permissions to existing systems without pushing SaaS concepts into core runtime state.",
        ],
      },
      {
        eyebrow: "Migration bridge",
        title: "Keep a team skill library alive while you migrate",
        desc: "Layer local SKILL.md packs and ClawHub-style bundles onto the runtime without forking the core or freezing the team during migration.",
        outcomes: [
          "Discover skills from disk at startup and refresh them in place.",
          "Use skill.ensure to recover missing capability during a run.",
          "Keep team knowledge versioned in reusable packs rather than prompt fragments.",
        ],
      },
    ],
    modesEyebrow: "Deployment shapes",
    modesTitle: "Three common entry paths: personal, team, and enterprise",
    modesDesc:
      "The same runtime can begin on a laptop and later expand into team or enterprise environments. The key is being explicit about which path you are taking first.",
    modes: [
      {
        title: "Personal local runtime",
        desc: "web-first onboarding, a local dashboard, browser or desktop helpers on demand, and skills from the workspace.",
      },
      {
        title: "Team self-hosted runtime",
        desc: "channel adapters, approval queues, durable state, production profile, and shared workflow execution.",
      },
      {
        title: "Enterprise / internal service",
        desc: "HTTP-driven run creation, approval hooks, authz and audit, health endpoints, and connection to existing business permission logic while HopClaw stays focused on execution.",
      },
    ],
  },
  docs: {
    badge: "Install, CLI, and deployment docs",
    title: "Open the documents that turn interest into a real install or deployment",
    desc: "This repository keeps docs intentionally small. Start from the files that matter most for personal setup, CLI usage, and enterprise rollout.",
    sourcesEyebrow: "Start here",
    sourcesTitle: "Read these first",
    sourcesDesc:
      "These are the best entry points when you want release-grade documentation instead of more marketing copy.",
    sources: [
      {
        title: "Quick Start",
        desc: "Install, onboard, manual five-minute path, and the first checks that prove the runtime is real.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/getting-started/quickstart.md",
        cta: "Open Quick Start",
      },
      {
        title: "CLI Reference",
        desc: "Interactive REPL, `serve`, target management, dashboard, doctor, and operator commands in one reference.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/reference/cli.md",
        cta: "Open CLI Reference",
      },
      {
        title: "Enterprise Webhook Quickstart",
        desc: "No-core-patch path for external AuthZ, audit forwarding, and bridge-based enterprise integration.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/enterprise-webhook-quickstart.md",
        cta: "Open Webhook Quickstart",
      },
    ],
    quickStartTitle: "Install and start",
    quickStartDesc:
      "Start with the one-line installer, then open the REPL with `hopclaw`. The Unix installer falls back to `~/.local/bin` when `/usr/local/bin` is not writable.",
    configEyebrow: "Config surface",
    configTitle: "Configuration is part of the operator contract",
    configDesc:
      "Skills, hosts, auth, and runtime profile belong in config where users can reason about them.",
    apiEyebrow: "API guide",
    apiTitle: "Key endpoints",
    apiDesc:
      "The same semantics power both the dashboard and external integrations.",
    method: "Method",
    endpoint: "Endpoint",
    description: "Description",
    apiRows: [
      {
        method: "GET",
        path: "/healthz",
        desc: "Readiness probe for monitors or local smoke tests.",
      },
      {
        method: "GET",
        path: "/runtime/tools",
        desc: "List the tools visible to the runtime in the current environment.",
      },
      {
        method: "POST",
        path: "/runtime/runs",
        desc: "Create and enqueue a new run for execution.",
      },
      {
        method: "GET",
        path: "/runtime/runs/:id",
        desc: "Fetch status, result, and related references for a run.",
      },
      {
        method: "GET",
        path: "/runtime/approvals",
        desc: "List pending approvals when work pauses for confirmation.",
      },
      {
        method: "POST",
        path: "/runtime/approvals/:id/resolve",
        desc: "Approve or deny a ticket and resume the paused run.",
      },
    ],
    guidesEyebrow: "Next docs",
    guidesTitle: "Guides worth opening next",
    guidesDesc:
      "After quick start, these are the references most useful to contributors, integrators, and operators.",
    guides: [
      {
        title: "README.md",
        desc: "Release boundary, helper model, profiles, and the overall project contract.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/README.md",
      },
      {
        title: "docs/README.md",
        desc: "The current docs map for the smaller release-grade documentation set kept in the repo.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/README.md",
      },
      {
        title: "Config Reference",
        desc: "Auth, approval, audit sinks, AuthZ, profiles, and integration-facing config surface.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/reference/config-reference.md",
      },
      {
        title: "runtime-v1.yaml",
        desc: "OpenAPI reference for the current Runtime HTTP API.",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/openapi/runtime-v1.yaml",
      },
    ],
  },
  telemetry: {
    badge: "Telemetry and trust",
    title: "Telemetry stays minimal, explicit, and best-effort",
    desc: "When outbound product telemetry is enabled, HopClaw sends anonymous install and adoption events to a collector you choose. Submission failure is silent by default and must not interrupt the runtime, onboarding, or normal installs.",
    facts: [
      "Outbound telemetry is opt-in and can stay fully disabled.",
      "Remote collection can use any trusted HTTPS endpoint that accepts the event schema.",
      "Self-hosted deployments can keep collection inside their own boundary.",
    ],
    primaryCta: "Open docs",
    endpointBadge: "Collector examples",
    endpointDesc:
      "Use either a remote HTTPS collector you trust or the built-in local collector route.",
    endpoints: [
      {
        label: "Remote ingest example",
        path: "https://telemetry.example.com/api/v1/ingest/events",
        note: "Example HTTPS endpoint for anonymous HopClaw product telemetry.",
      },
      {
        label: "Local collector route",
        path: "POST /telemetry/events",
        note: "Built-in route for a self-hosted gateway collector.",
      },
    ],
    payloadEyebrow: "Payload boundary",
    payloadTitle: "Count product adoption, not user content",
    payloadDesc:
      "Telemetry is for install and usage signals only. It is intentionally narrow so the product can report adoption without touching prompts or workspace data.",
    sendsTitle: "What is sent",
    sends: [
      "Anonymous install identifier generated on the local machine.",
      "Event name, event time, product version, release channel, OS, and CPU architecture.",
      "Minimal event properties such as activation surface, selected provider, plugin name, or skill id.",
      "Install and active-install signals used for release and ecosystem decisions.",
    ],
    neverTitle: "What is never sent",
    never: [
      "Prompt contents or conversation history.",
      "File contents, repository source, or local artifact bodies.",
      "Command bodies or terminal transcript contents.",
      "Local filesystem paths, API keys, or secret values.",
      "Seats, billing identity, or exact authenticated human user counts.",
    ],
    eventsEyebrow: "Current events",
    eventsTitle: "The current first-party event contract stays small",
    eventsDesc:
      "These are the product events currently emitted by HopClaw when telemetry is enabled.",
    events: [
      {
        name: "install.completed",
        desc: "A completed first activation path for a local install.",
      },
      {
        name: "onboard.completed",
        desc: "Guided setup reached completion with high-level setup properties only.",
      },
      {
        name: "runtime.active",
        desc: "A daily active-install signal emitted from the runtime surface.",
      },
      {
        name: "plugin.installed",
        desc: "A plugin install event with plugin name, version, and source kind.",
      },
      {
        name: "skill.installed",
        desc: "A skill install event with skill id, version, and source kind.",
      },
    ],
    behaviorEyebrow: "Failure semantics",
    behaviorTitle: "Telemetry must never degrade the product path",
    behaviorDesc:
      "The user-facing rule is simple: reporting is best-effort. Users should not see broken-flow behavior just because a telemetry endpoint is slow or unavailable.",
    behavior: [
      {
        title: "Silent by default",
        desc: "Submission failures are swallowed in normal use. Only `diagnostics.telemetry_debug_log: true` turns failure details into debug logs.",
      },
      {
        title: "Background where it matters",
        desc: "Serve, gateway, plugin install, and skill install reporting run in the background. Onboarding uses a very short silent timeout instead of a long blocking path.",
      },
      {
        title: "No product dependency",
        desc: "Runtime boot, onboarding completion, approvals, and installs continue even when the telemetry service is unreachable.",
      },
    ],
    configEyebrow: "Config choices",
    configTitle: "Choose the reporting boundary explicitly",
    configDesc:
      "Choose whether telemetry stays off, goes to a remote collector you trust, or stays local through the built-in collector path.",
    remoteLabel: "Remote collector",
    remoteTitle:
      "Send anonymous adoption metrics to a remote HTTPS endpoint",
    remoteDesc:
      "Use this when you want release and adoption metrics to flow to an external collector under an explicit endpoint choice.",
    localCollectorLabel: "Local collector",
    localCollectorTitle: "Keep telemetry inside your own boundary",
    localCollectorDesc:
      "Use the built-in collector path when outbound product analytics is disallowed and raw event storage must stay local.",
  },
  clawHub: {
    badge: "Migration and compatibility",
    title: "Move OpenClaw assets into a governed runtime",
    desc: "HopClaw keeps SKILL.md, `.openclaw` roots, and common install results usable so teams can migrate without freezing work. The goal is not nostalgia. The goal is lower migration cost into approvals, audit, artifacts, and operator APIs.",
    heroPoints: [
      "Keep project-local and user-global SKILL.md packs readable.",
      "Reuse `~/.openclaw` roots and workspace-style directories while you move workflows over.",
      "Hot-reload compatible skills and govern install behavior through ask, auto, or deny.",
    ],
    pillarsEyebrow: "What migrates cleanly",
    pillarsTitle: "Carry the assets that matter",
    pillarsDesc:
      "This is not a one-time importer. It is a working migration path for the places teams already keep and share skills.",
    pillars: [
      {
        title: "Direct SKILL.md loading",
        desc: "Project-local and user-global skill packs can stay on disk and bind into the runtime without repackaging the core binary.",
      },
      {
        title: "OpenClaw root reuse",
        desc: "Compatibility covers `./skills`, `~/.openclaw/skills`, and workspace-style roots instead of forcing a new storage convention.",
      },
      {
        title: "Hot refresh and ensure",
        desc: "The runtime can watch compatible roots, refresh them live, and recover missing capability through governed skill.ensure.",
      },
    ],
    policyEyebrow: "Install policy",
    policyTitle: "Make install behavior visible",
    policyDesc:
      "Missing capability should resolve through explicit policy, not quiet magic. ask, auto, and deny define the user experience clearly.",
    policies: [
      {
        title: "ask",
        desc: "Create an approval ticket when the agent needs to install or ensure a skill during a run.",
      },
      {
        title: "auto",
        desc: "Proceed without pausing when the runtime is configured to allow automatic skill installation.",
      },
      {
        title: "deny",
        desc: "Refuse runtime installation and require the agent to explain the missing capability instead.",
      },
    ],
    groupsEyebrow: "Compatibility roots",
    groupsTitle: "The roots and bundle paths that matter",
    groupsDesc:
      "These are the roots and bundle shapes worth preserving during migration. They are not the whole product.",
    groups: [
      {
        title: "Project-local skills",
        desc: "Keep team-owned SKILL.md packs in `./skills` and let the runtime discover them with the same operator surface as built-ins.",
      },
      {
        title: "User-global roots",
        desc: "Reuse `~/.openclaw/skills` and other familiar personal roots so a user library does not need to be reauthored.",
      },
      {
        title: "Workspace skill folders",
        desc: "Honor workspace-style compatible directories so multi-repo or per-workspace skill layouts remain practical.",
      },
      {
        title: "ClawHub bundles and thin bridge",
        desc: "Keep ClawHub-style install results usable and add a thin manifest/provider bridge for the most common extension entry path.",
      },
    ],
    authorEyebrow: "Author flow",
    authorTitle: "Keep migration live while the team keeps shipping",
    authorDesc:
      "The goal is to let skill authors keep iterating in place: version files, reload roots, and ship through the same ecosystem patterns while the runtime surface grows stricter.",
    authorSteps: [
      {
        title: "Drop in SKILL.md",
        desc: "Keep capability in versioned files close to the workspace or user-global roots where teams already manage skill assets.",
      },
      {
        title: "Refresh without restart",
        desc: "Use auto-detect and auto-refresh so edits to compatible roots become visible without rebooting the runtime.",
      },
      {
        title: "Distribute through the same ecosystem",
        desc: "Share the pack from your repo or a ClawHub-compatible bundle instead of forking HopClaw for every new capability.",
      },
    ],
    note: "Today the strongest shipped migration path is local and ClawHub-compatible skill reuse with hot reload. Broader external plugin-host work continues on a separate track.",
  },
};
