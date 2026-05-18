export default {
  nav: {
    runtime: "运行机制",
    features: "能力边界",
    useCases: "使用场景",
    docs: "文档",
    install: "立即安装",
    telemetry: "上报说明",
    skills: "迁移",
    github: "GitHub",
    tagline: "先自己用，再扩到团队",
  },
  ui: {
    copy: "复制",
    copied: "已复制",
  },
  footer: {
    blurb:
      "HopClaw 是一个用 Go 构建的 Agent Runtime。你可以先在自己电脑上用起来，等价值清楚后，再扩到团队工作流和企业接入。",
    note: "当前版本的核心价值是实用性：REPL、dashboard、浏览器和桌面能力、文件与命令工具、审批、审计、API，以及对既有 SKILL.md 资产的迁移桥。",
    license: "Apache-2.0 · Go 构建的 Agent Runtime",
    product: "官方页面",
    resources: "事实依据",
    repo: "共建入口",
    quickstart: "快速开始",
    cli: "CLI 参考",
    enterpriseDeploy: "企业部署",
    telemetry: "上报说明",
    readme: "README",
    readmeCN: "README（中文）",
    docsIndex: "文档索引",
    configExample: "config.example.yaml",
    issues: "问题反馈",
    releases: "发布版本",
    bugTemplate: "Bug 模板",
    featureTemplate: "需求模板",
    contributing: "贡献指南",
    copyright: "© 2026 HopClaw。由 Go 构建。",
  },
  layers: {
    badge: "运行面地图",
    cards: [
      {
        tone: "runtime",
        badge: "治理核心",
        title: "执行、审批与审计收在一个运行时里",
        desc: "run、session、tool dispatch、approval、artifact、audit 以及 HTTP 控制面都收在主 Go Runtime 内部。",
        tags: ["runs", "approvals", "artifacts", "audit"],
      },
      {
        tone: "system",
        badge: "Channels / Hosts",
        title: "聊天、浏览器、桌面与系统能力按环境暴露",
        desc: "channels、Layer 2 groups、browser helper、desktop helper 只有在当前环境真实可用时才会被暴露。",
        tags: ["git.*", "container.*", "browser.*", "desktop.*"],
      },
      {
        tone: "skills",
        badge: "迁移桥",
        title: "把既有技能资产带进来",
        desc: "HopClaw 可以从熟悉的兼容目录读取 SKILL.md，支持热刷新，并通过受治理的 skill.ensure 恢复缺失能力。",
        tags: ["SKILL.md", "~/.openclaw", "ClawHub bundles", "skill.ensure"],
      },
    ],
    flowProbe: "ingress -> runtime -> policy -> operator APIs",
    flowSkill: "skill roots -> ensure/install -> approval or policy",
  },
  home: {
    badge: "暖色、实用、先从你自己的电脑开始",
    titleLines: [
      "装上 HopClaw，让 AI 真正帮你做事。",
      "先简单用起来，之后再扩到团队。",
    ],
    desc: "HopClaw 是一个用 Go 构建的 Agent Runtime，适合想把 AI 用在真实电脑工作上的人。你可以先本地使用浏览器、桌面、文件和命令；等团队需要时，再用同一套核心去接服务、API、审批和审计。",
    heroPoints: [
      "先给自己用，不需要先搭很复杂的系统",
      "能打开网页、处理文件、执行命令，而且结果可回看",
      "需要团队或企业接入时，再按需开启更强的控制能力",
    ],
    primaryCta: "5 分钟安装",
    secondaryCta: "查看使用场景",
    installBadge: "快速开始",
    installTitle: "从这里开始",
    installOptions: [
      {
        id: "unix",
        label: "macOS / Linux",
        desc: "一条命令安装 HopClaw，并进入本地引导流程。",
        command:
          "curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh",
      },
      {
        id: "windows",
        label: "Windows",
        desc: "PowerShell 一键安装，走同一套引导流程。",
        command:
          "$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex",
      },
    ],
    installChecks: [
      {
        step: "01",
        title: "安装",
        desc: "运行安装命令，让 HopClaw 建好第一套本地运行环境。",
      },
      {
        step: "02",
        title: "打开 HopClaw",
        desc: "进入交互式客户端，并在 dashboard 里完成初始设置。",
      },
      {
        step: "03",
        title: "跑一个真实任务",
        desc: "试一次浏览器、文件或命令流程，看看它是否真的省你的时间。",
      },
    ],
    valueEyebrow: "为什么用户会继续留下来",
    valueTitle: "不是更会说，而是真的更有用",
    valueDesc: "第一次体验应该立刻有价值，而且后续依然清楚、可控。",
    valueCards: [
      {
        badge: "能动手",
        title: "它能执行，不只是会回答",
        desc: "用同一套 runtime 处理浏览器步骤、桌面动作、文件、命令和可回查结果。",
      },
      {
        badge: "看得见",
        title: "你能知道刚刚发生了什么",
        desc: "dashboard、health、doctor、runs 和 artifacts 让结果更容易检查，而不是只能靠猜。",
      },
      {
        badge: "能扩展",
        title: "先个人使用，之后再给团队",
        desc: "本地安装不会把你锁死在个人模式里，后续仍可扩到自托管、API 或企业接入。",
      },
    ],
    expandEyebrow: "从个人使用到团队落地",
    expandTitle: "同一套产品，分两步使用",
    expandDesc: "大多数人应该先本地用起来。等价值跑出来之后，再把同一套 runtime 扩到团队和企业场景。",
    personalPath: {
      badge: "个人使用",
      title: "先本地用，先把事情做起来",
      desc: "适合开发者、研究者、运维、写作者和重度电脑用户，先把 AI 变成你电脑上的实用工具。",
      points: [
        "运行 `hopclaw` 进入交互式客户端",
        "需要时再接 browser 或 desktop helper",
        "遇到问题先用 `hopclaw doctor` 排查",
      ],
    },
    teamPath: {
      badge: "团队与企业",
      title: "需要服务化时，再平滑扩出去",
      desc: "当你需要服务模式、API、审批和审计时，HopClaw 已经提供了清晰的自托管路径。",
      points: [
        "用 `hopclaw serve` 作为服务入口",
        "从官方企业部署包起步",
        "租户、组织和 RBAC 业务逻辑继续放在你自己的系统里",
      ],
    },
    enterpriseCards: [
      {
        badge: "审批",
        title: "需要时，可以拦住高风险动作",
        desc: "对于写操作或高风险执行，可以接上审批流程，而不是默认放开。",
      },
      {
        badge: "审计",
        title: "进入生产后，保留清晰审计链路",
        desc: "审计外发支持可靠交付，避免企业部署只能停留在 best-effort 日志层。",
      },
      {
        badge: "部署",
        title: "官方部署包就是起点，不必从零拼装",
        desc: "compose、示例配置、HTTPS 边缘、告警规则和 runbook 都已在仓库里。",
      },
    ],
    ctaEyebrow: "可以开始试了",
    ctaTitle: "先安装，再用一个真实任务判断它值不值得留下",
    ctaDesc: "评估 HopClaw 最好的方法，不是继续看文章，而是装起来跑一次真实流程，看看它能不能进入你的日常工作。",
    ctaPrimary: "立即安装",
    ctaSecondary: "前往 GitHub",
  },
  runtime: {
    badge: "运行时契约",
    title: "一套把执行过程持续公开出来的运行时",
    desc: "从 ingress 到结果落盘，HopClaw 都尽量让面向团队的执行过程可读：什么开始了、什么暂停了、什么恢复了、什么产出了 artifact、当前到底有哪些 tools 可用。",
    mapEyebrow: "请求路径",
    mapTitle: "操作员真正依赖的是这四层",
    mapDesc: "入口、执行、治理和可选宿主共同决定了运行时是否真的可运维。",
    mapCards: [
      {
        title: "Ingress",
        desc: "channels 和 HTTP 负责产生工作、绑定 session、入队 runs，而不是把传输层规则混进 agent loop。",
      },
      {
        title: "Execution core",
        desc: "运行时负责准备上下文、路由模型、调度 tools、落盘 artifact，并发布 events，形成统一服务契约。",
      },
      {
        title: "Governance",
        desc: "approval、audit、install policy 和 runtime profile 定义高风险工作如何暂停、恢复，以及如何被解释。",
      },
      {
        title: "Optional hosts",
        desc: "browser.* 和 desktop.* 会保持在 helper 之外，只有 helper 配置正确、认证正常且健康时才接入核心运行时。",
      },
    ],
    bootEyebrow: "启动契约",
    bootTitle: "启动时就把真实运行面宣告出来",
    bootDesc:
      "可信的 runtime 必须在第一个 run 开始前，就说明 profile、channels、hosts、tools 和审批语义。",
    bootPoints: [
      "注册内置 tools 与当前真正有效的 Layer 2 groups。",
      "检测已配置 hosts、兼容 roots 和当前 runtime profile。",
      "通过 HTTP 把 tools 与 operator endpoints 用明确的 readiness state 暴露出来。",
    ],
    flowEyebrow: "Run 生命周期",
    flowTitle: "聊天、HTTP 与 operator workflow 共用同一条生命周期",
    flowDesc:
      "无论任务从哪里进来，运行时都维持同一执行形状，这样状态与副作用才能持续可检查。",
    flowSteps: [
      {
        step: "01",
        title: "创建 run",
        desc: "入站请求先绑定 session key 并持久化，再开始执行。",
      },
      {
        step: "02",
        title: "准备上下文",
        desc: "运行时会压缩历史、应用 token 预算，并构造面向模型的消息窗口。",
      },
      {
        step: "03",
        title: "调用 tools",
        desc: "内置工具、宿主工具和兼容 skills 在同一套运行时契约下执行。",
      },
      {
        step: "04",
        title: "拦截副作用",
        desc: "高风险操作会停在 approval 或 policy 门后，而不是把信任边界交给模型自己定义。",
      },
      {
        step: "05",
        title: "持久化输出",
        desc: "大结果会变成 artifact 与 events，方便操作员事后检查。",
      },
      {
        step: "06",
        title: "恢复或结束",
        desc: "暂停的工作会在显式决策后恢复，否则 run 会以可查询的最终状态结束。",
      },
    ],
    profilesEyebrow: "Profiles",
    profilesTitle: "会真正改变行为的 Profiles",
    profilesDesc:
      "这些 profile 会具体改动默认行为，而不是挂一个模糊的“生产模式”标签。",
    profiles: [
      {
        name: "desktop",
        desc: "默认本地优先 profile，同时仍然保留对写类或高风险操作的审批门。",
      },
      {
        name: "trusted_desktop",
        desc: "在可信个人机器上降低摩擦，但不会把破坏性行为悄悄变成默认路径。",
      },
      {
        name: "production",
        desc: "要求认证、持久状态、更严格的 exec 默认值，以及带审计的启动前检查。",
      },
    ],
    boundaryEyebrow: "当前范围",
    boundaryTitle: "今天已经交付什么，哪些还明确不做",
    boundaryDesc:
      "现在这套运行时表面已经足够进入真实工作流，但更大一层的平台能力仍然明确留在范围外。",
    shippedTitle: "现在就有",
    shipped: [
      "run 与 session 生命周期、approval、artifact、audit、event bus。",
      "HTTP runtime API、本地 dashboard 与核心 operator workflows。",
      "内置 tools、Layer 2 工具组，以及 browser/desktop 宿主暴露。",
      "面向迁移的 skill 加载、热刷新和受策略治理的 ensure/install。",
      "覆盖团队聊天、消息入口、webhook，以及常见 productivity skills / bundles 的接入面。",
    ],
    notShippedTitle: "仍在扩展",
    notShipped: [
      "更广义的 gRPC plugin host 与协议表面。",
      "更多语言与客户端的自动生成 SDK。",
      "更深的 browser orchestration，例如下载与更完整的多标签流程。",
      "覆盖所有长任务场景的更完整 operator console workflows。",
    ],
  },
  features: {
    badge: "运行时表面",
    title: "不是只给 prompt builder 用的，而是给 operator 用的",
    desc: "HopClaw 的目标，是在智能体接触 shell、文件、聊天系统、browser、desktop 和真实业务工具时，仍让运行面保持清晰。",
    pillarsEyebrow: "核心收益",
    pillarsTitle: "开箱可用的核心能力",
    pillarsDesc: "重点不是新概念，而是把真实工作里最关键的边界和控制点做清楚。",
    pillars: [
      {
        title: "治理执行",
        desc: "审批、审计、artifact 与 operator 可见状态都收进 runtime，而不是继续散落在 prompt 里。",
      },
      {
        title: "可见运行时状态",
        desc: "runs、approvals、artifacts 和当前 tools 暴露都能被查询。",
      },
      {
        title: "渠道与宿主覆盖",
        desc: "聊天入口、browser helper、desktop helper 与 office workflow 都接进同一套 runtime contract。",
      },
      {
        title: "迁移桥",
        desc: "团队可以把 SKILL.md 资产带进来，但兼容不再是全部产品身份。",
      },
    ],
    toolsEyebrow: "能力来源",
    toolsTitle: "一个运行时表面，多个能力来源",
    toolsDesc:
      "内置、Layer 2、hosts、channels 和 skills 会按真实可用、可治理的状态被区分，而不是拍平成一套模糊的 plugin 叙事。",
    toolFamilies: [
      {
        title: "核心工具",
        desc: "文件、命令、网络、文本、运行时、审计与通用工具直接编进 Go 二进制。",
      },
      {
        title: "Layer 2 工具组",
        desc: "git、packages、containers、search、speech、media 等依赖系统环境的能力组。",
      },
      {
        title: "Browser 宿主",
        desc: "通过 hopclaw-browserd 提供 navigate、click、type、wait、snapshot、screenshot 和 tab 生命周期。",
      },
      {
        title: "Desktop 宿主",
        desc: "通过 hopclaw-desktopd 提供窗口聚焦、树捕获、热键、截图和剪贴板流。",
      },
      {
        title: "渠道接入",
        desc: "聊天和 webhook 适配器进入同一套 run 生命周期。",
      },
      {
        title: "兼容 skills",
        desc: "本地 SKILL.md 发现，加上运行时里的 ensure/install 策略，用于恢复 GitHub、Notion、Jira、Trello、Slack、email、Feishu/Lark 等常见工作能力。",
      },
    ],
    securityEyebrow: "风险控制",
    securityTitle: "风险控制是 runtime 内建能力",
    securityDesc: "风险管理必须能够离开对话窗口独立存在。",
    securityCards: [
      {
        title: "审批单",
        desc: "潜在高风险操作会生成运行时审批单，可被人工处理后再恢复。",
      },
      {
        title: "审计链路",
        desc: "工具执行、审批以及相关安全事件都会被持久化，便于事后追踪。",
      },
      {
        title: "运行配置档",
        desc: "desktop、trusted_desktop、production 会在第一条 run 之前就改变默认行为。",
      },
      {
        title: "用 artifact 替代聊天刷屏",
        desc: "大输出会以可引用形式保存，而不是把模型窗口与操作记录全部淹没。",
      },
    ],
    contextEyebrow: "上下文控制",
    contextTitle: "上下文边界是故意被控制住的",
    contextDesc:
      "长期运行任务之所以还能保持可用，是因为运行时会主动压缩历史、预留输出空间，并裁剪工具结果。",
    contextNotes: [
      "滑动窗口压缩会保留系统策略、最早锚点和最新消息，并对中间部分做滚动摘要。",
      "token 预算会预留输出空间，而不是让长输入吃掉整个模型窗口。",
      "大工具结果会做 soft-trim，并可持久化为 artifact 供后续检查。",
    ],
    contextBands: [
      "system policy",
      "oldest anchors",
      "rolling summary",
      "latest messages",
      "reserved output",
    ],
    surfaceEyebrow: "API 表面",
    surfaceTitle: "HTTP 控制面本身就是产品的一部分",
    surfaceDesc:
      "Dashboard 只是一个视图，真正稳定耐用的 operator contract 是下面的 API。",
    area: "区域",
    surface: "Endpoint / 表面",
    whenToUse: "适用场景",
    surfaceRows: [
      { area: "Health", path: "GET /healthz", desc: "健康检查与冒烟探测。" },
      {
        area: "Runs",
        path: "POST /runtime/runs",
        desc: "从自有系统创建或入队新的工作。",
      },
      {
        area: "Status",
        path: "GET /runtime/runs/:id",
        desc: "查询 run 状态、结果和已存储引用。",
      },
      {
        area: "Approvals",
        path: "GET /runtime/approvals",
        desc: "查看待处理审批与操作员积压。",
      },
      {
        area: "Artifacts",
        path: "GET /runtime/artifacts",
        desc: "不重放任务，也能检查大输出。",
      },
      {
        area: "Tools",
        path: "GET /runtime/tools",
        desc: "直接发现当前环境下运行时真正能做什么。",
      },
    ],
  },
  useCases: {
    badge: "适用场景",
    title: "从个人桌面到团队流程，HopClaw 放在哪些真实工作流里",
    desc: "它不是只给小团队做 prompt / skill / tool 资产管控的平台，也不只是 chat bot。你可以把它当个人桌面 agent runtime、团队 workflow 执行层，或者企业内部通过 HTTP 和 hooks 接入的治理型 runtime。",
    cases: [
      {
        eyebrow: "个人桌面",
        title: "先在自己的机器上把桌面、浏览器和系统工具跑起来",
        desc: "对个人开发者或重度电脑用户来说，HopClaw 最直接的价值不是团队协作，而是把 browser.*、desktop.*、文件、命令和知识流程收进一个本地 runtime。",
        outcomes: [
          "当 Web 产品没有好用 API 时，使用 browser.*。",
          "当任务需要截图、热键、窗口聚焦和剪贴板时，使用 desktop.*。",
          "本地也能通过 dashboard、doctor 和 artifact 看清状态。",
        ],
      },
      {
        eyebrow: "团队 ChatOps",
        title: "把发版、运维和协作助手放进审批门之后",
        desc: "当助手需要从 Slack、Discord、Telegram 或飞书做部署、查日志、改基础设施时，channel adapter 加策略门控比空谈“更自主”更重要。",
        outcomes: [
          "多个聊天产品共享同一套 runtime surface。",
          "高风险工具先暂停，人工批准后再恢复。",
          "真正能追查“改了什么”，而不是只看聊天记录。",
        ],
      },
      {
        eyebrow: "企业接入",
        title: "从你自己的系统、调度器或内网平台里驱动 runs",
        desc: "如果你已经有调度器、Webhook 源或者内部平台，HopClaw 可以稳稳地挂在 HTTP 后面，并接 approval hook / authz 逻辑，而不把聊天当成唯一控制面。它负责执行，你自己的系统继续保留租户和权限逻辑。",
        outcomes: [
          "异步创建 run，之后再轮询结果。",
          "通过 HTTP 检查 approvals、artifacts、tools 和运行状态。",
          "把审批、审计和业务权限逻辑接回现有系统，而不是把 SaaS 概念压进核心 runtime 状态。",
        ],
      },
      {
        eyebrow: "Migration bridge",
        title: "迁移过程中继续保持团队技能库可用",
        desc: "把本地 SKILL.md packs 与 ClawHub 风格 bundle 叠加到运行时之上，无需 fork 核心，也不用因为迁移冻结团队工作流。",
        outcomes: [
          "启动时从磁盘发现技能，并支持原地热刷新。",
          "运行中通过 skill.ensure 恢复缺失能力。",
          "把团队知识保存在可版本化、可复用的 packs 中，而不是散在 prompt 片段里。",
        ],
      },
    ],
    modesEyebrow: "部署形态",
    modesTitle: "个人、团队、企业三条常见进入路径",
    modesDesc:
      "同一套 runtime 可以先跑在个人电脑上，再扩到团队或企业环境，关键是先明确你要走哪条路径。",
    modes: [
      {
        title: "个人本地",
        desc: "web-first onboarding、本地 dashboard、按需接 browser / desktop helper，以及 workspace 内的 skills。",
      },
      {
        title: "团队自托管",
        desc: "channel adapters、审批队列、持久状态、production profile，以及团队共享的 workflow。",
      },
      {
        title: "企业 / 内网接入",
        desc: "HTTP 驱动的 run 创建、approval hooks、authz / audit、健康检查，以及接入既有业务权限逻辑，同时让 HopClaw 保持执行内核定位。",
      },
    ],
  },
  docs: {
    badge: "安装、CLI 与部署文档",
    title: "优先打开那些能把兴趣变成真实安装或部署的文档",
    desc: "仓库里的文档保持精简。先看最能支撑个人上手、CLI 使用和企业落地的那些文件。",
    sourcesEyebrow: "先看这里",
    sourcesTitle: "最值得先读的文档",
    sourcesDesc: "如果你不想再看营销文案，而想直接判断真实边界，先从这些文件开始。",
    sources: [
      {
        title: "Quick Start",
        desc: "安装、onboard、手动五分钟路径，以及第一轮验证 runtime 是否真的成立的方法。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/getting-started/quickstart.md",
        cta: "打开 Quick Start",
      },
      {
        title: "CLI Reference",
        desc: "交互式 REPL、`serve`、target 管理、dashboard、doctor 和 operator 命令的统一参考。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/reference/cli.md",
        cta: "打开 CLI 参考",
      },
      {
        title: "Enterprise Webhook Quickstart",
        desc: "不改核心代码即可接外部 AuthZ、审计外发和企业桥接器的最短路径。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/enterprise-webhook-quickstart.md",
        cta: "打开 Webhook Quickstart",
      },
    ],
    quickStartTitle: "安装与上手",
    quickStartDesc:
      "先用一行安装器把 HopClaw 装起来，再直接运行 `hopclaw` 进入 REPL；Unix 安装器在 `/usr/local/bin` 不可写时会自动退回到 `~/.local/bin`。",
    configEyebrow: "配置表面",
    configTitle: "配置本身就是运行契约的一部分",
    configDesc:
      "skills、hosts、auth 和 runtime profile 应该放在配置里，而不是埋进 prompt。",
    apiEyebrow: "API 导览",
    apiTitle: "关键端点",
    apiDesc: "这些语义同时支撑 dashboard 和外部集成。",
    method: "Method",
    endpoint: "Endpoint",
    description: "说明",
    apiRows: [
      {
        method: "GET",
        path: "/healthz",
        desc: "健康探针，适合监控或本地冒烟测试。",
      },
      {
        method: "GET",
        path: "/runtime/tools",
        desc: "列出当前环境下运行时实际可见的工具。",
      },
      { method: "POST", path: "/runtime/runs", desc: "创建并入队新的 run。" },
      {
        method: "GET",
        path: "/runtime/runs/:id",
        desc: "查询 run 的状态、结果和相关引用。",
      },
      {
        method: "GET",
        path: "/runtime/approvals",
        desc: "当工作因确认暂停时，列出待处理审批。",
      },
      {
        method: "POST",
        path: "/runtime/approvals/:id/resolve",
        desc: "批准或拒绝审批单，并恢复暂停中的 run。",
      },
    ],
    guidesEyebrow: "后续文档",
    guidesTitle: "快速开始之后最该打开的文档",
    guidesDesc:
      "跑起来之后，这几份文档最适合继续做集成、扩展和运维接入。",
    guides: [
      {
        title: "README.md",
        desc: "版本边界、helper 模型、profiles 和总体项目契约。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/README.md",
      },
      {
        title: "docs/README.md",
        desc: "仓库里当前保留的精简发布级文档地图。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/README.md",
      },
      {
        title: "Config Reference",
        desc: "auth、approval、audit sinks、AuthZ、profiles 与对外接入配置表面。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/reference/config-reference.md",
      },
      {
        title: "runtime-v1.yaml",
        desc: "当前 Runtime HTTP API 的 OpenAPI 参考。",
        href: "https://github.com/fulcrus/hopclaw/blob/main/docs/openapi/runtime-v1.yaml",
      },
    ],
  },
  telemetry: {
    badge: "上报与信任边界",
    title: "telemetry 保持最小、显式，而且是 best-effort",
    desc: "当启用产品 telemetry 时，HopClaw 只会把匿名安装与采用情况上报到你明确选择的采集端点。上报失败默认静默，不能打断 runtime、onboarding 或正常安装流程。",
    facts: [
      "出站 telemetry 是显式开启的，也可以始终保持关闭。",
      "远程采集可以使用任意你信任、且兼容事件结构的 HTTPS 端点。",
      "自托管部署可以把采集完全留在自己的边界内。",
    ],
    primaryCta: "打开文档",
    endpointBadge: "采集端示例",
    endpointDesc:
      "可以使用你信任的远程 HTTPS 采集端，也可以使用内建本地 collector 路径。",
    endpoints: [
      {
        label: "远程上报示例",
        path: "https://telemetry.example.com/api/v1/ingest/events",
        note: "匿名产品 telemetry 的 HTTPS 采集端示例。",
      },
      {
        label: "本地 collector 路径",
        path: "POST /telemetry/events",
        note: "自托管 gateway 可直接使用的内建采集路由。",
      },
    ],
    payloadEyebrow: "数据边界",
    payloadTitle: "统计的是产品采用，不是用户内容",
    payloadDesc:
      "telemetry 只用于安装和使用信号，范围故意做得很窄，让产品能知道 adoption 情况，同时不碰 prompt 或工作区内容。",
    sendsTitle: "会发送什么",
    sends: [
      "由本地机器生成的匿名 install id。",
      "事件名、事件时间、产品版本、发布通道、操作系统和 CPU 架构。",
      "最小事件属性，例如启动入口、选中的 provider、plugin 名称或 skill id。",
      "用于发布判断和生态判断的安装量、活跃安装量等信号。",
    ],
    neverTitle: "绝不会发送什么",
    never: [
      "Prompt 内容或对话历史。",
      "文件内容、仓库源码或本地产物正文。",
      "命令正文或终端输出正文。",
      "本地文件路径、API key 或其他 secret 值。",
      "座席、计费身份，或精确到人的认证用户数。",
    ],
    eventsEyebrow: "当前事件",
    eventsTitle: "当前一方事件契约保持得很小",
    eventsDesc: "开启 telemetry 后，HopClaw 当前会上报这些产品事件。",
    events: [
      { name: "install.completed", desc: "本地安装第一次完成激活路径时记录。" },
      {
        name: "onboard.completed",
        desc: "引导配置完成时记录，只带高层配置属性。",
      },
      {
        name: "runtime.active",
        desc: "从 runtime 表面发出的每日活跃安装信号。",
      },
      {
        name: "plugin.installed",
        desc: "plugin 安装事件，包含名称、版本和来源类型。",
      },
      {
        name: "skill.installed",
        desc: "skill 安装事件，包含 skill id、版本和来源类型。",
      },
    ],
    behaviorEyebrow: "失败语义",
    behaviorTitle: "上报绝不能拖累产品主路径",
    behaviorDesc:
      "面向用户的规则很简单：上报只是 best-effort。即使 telemetry 服务变慢或不可用，用户也不应该感受到主流程被打断。",
    behavior: [
      {
        title: "默认静默",
        desc: "正常使用下会吞掉上报失败。只有显式打开 `diagnostics.telemetry_debug_log: true` 才会输出调试级失败日志。",
      },
      {
        title: "该后台就后台",
        desc: "serve、gateway、plugin 安装和 skill 安装的上报走后台。onboarding 则使用一个极短的静默超时，而不是长时间阻塞。",
      },
      {
        title: "产品不依赖上报成功",
        desc: "runtime 启动、onboarding 结束、审批流和安装流程，在 telemetry 服务不可达时都继续正常运行。",
      },
    ],
    configEyebrow: "配置选择",
    configTitle: "显式选择上报边界",
    configDesc:
      "可以显式选择关闭 telemetry、上报到你信任的远程采集端，或者通过内建 collector 路径把数据留在本地。",
    remoteLabel: "远程采集端",
    remoteTitle: "把匿名采用指标上报到远程 HTTPS 端点",
    remoteDesc:
      "适合需要把版本与采用指标汇总到外部采集端的场景，端点由你自己明确配置。",
    localCollectorLabel: "本地 collector",
    localCollectorTitle: "把 telemetry 留在自己的边界内",
    localCollectorDesc:
      "当不允许出站产品分析时，可使用内建 collector 路径，把原始事件完全留在本地。",
  },
  clawHub: {
    badge: "迁移与兼容",
    title: "把 OpenClaw 资产迁进受治理的 runtime",
    desc: "HopClaw 会继续识别 SKILL.md、`.openclaw` roots 和常见安装结果，让团队迁移时不必冻结工作。重点不是怀旧，而是用更低迁移成本换来 approval、audit、artifact 和 operator API。",
    heroPoints: [
      "项目内与个人目录下的 SKILL.md packs 可以继续读取。",
      "继续复用 `~/.openclaw` roots 与 workspace 风格目录，同时逐步迁移工作流。",
      "兼容 skills 支持热刷新，安装行为通过 ask、auto、deny 管理。",
    ],
    pillarsEyebrow: "可平滑迁移的内容",
    pillarsTitle: "先把真正重要的资产带过来",
    pillarsDesc:
      "这不是一次性 importer，而是一条可持续的迁移路径，让团队继续沿用原来存放和共享 skills 的地方。",
    pillars: [
      {
        title: "直接读取 SKILL.md",
        desc: "项目内和个人目录下的技能包可以继续放在磁盘上，由运行时直接绑定。",
      },
      {
        title: "复用 OpenClaw 根目录",
        desc: "兼容覆盖 `./skills`、`~/.openclaw/skills` 和 workspace 风格目录。",
      },
      {
        title: "热刷新与 ensure",
        desc: "运行时能监听兼容 roots、实时刷新，并通过受策略控制的 skill.ensure 恢复缺失能力。",
      },
    ],
    policyEyebrow: "安装策略",
    policyTitle: "让安装行为变得可见",
    policyDesc:
      "缺失能力出现时，ask、auto、deny 会明确决定安装行为和运行方式。",
    policies: [
      {
        title: "ask",
        desc: "当智能体在运行中需要安装或确保某个技能时，先创建审批单。",
      },
      {
        title: "auto",
        desc: "如果运行时被配置为允许自动安装，就不暂停，直接继续。",
      },
      {
        title: "deny",
        desc: "拒绝运行期安装，并要求智能体解释当前缺少的能力。",
      },
    ],
    groupsEyebrow: "兼容根目录",
    groupsTitle: "最关键的根目录与 bundle 形态",
    groupsDesc:
      "这些 roots 和 bundle 形态值得优先保留，但它们不是全部产品定义。",
    groups: [
      {
        title: "项目内 skills",
        desc: "把团队自有 SKILL.md packs 放在 `./skills` 下，让它们和内置能力一样接入统一控制面。",
      },
      {
        title: "个人 roots",
        desc: "复用 `~/.openclaw/skills` 以及其他熟悉的个人根目录，避免个人技能库被迫重写。",
      },
      {
        title: "Workspace skill folders",
        desc: "兼容 workspace 风格目录，让多仓库或多工作区的技能布局继续可用。",
      },
      {
        title: "ClawHub bundles 与薄兼容桥",
        desc: "保留 ClawHub 风格安装结果的可用性，并为最常用扩展入口补一层轻量 manifest/provider 兼容桥。",
      },
    ],
    authorEyebrow: "作者工作流",
    authorTitle: "让迁移持续进行，同时团队继续交付",
    authorDesc:
      "skill 作者可以继续在原地迭代：版本化文件、热刷新根目录，并沿用同一套生态分发方式，同时让 runtime surface 变得更严格。",
    authorSteps: [
      {
        title: "直接放入 SKILL.md",
        desc: "把能力保存在版本化文件里，贴近 workspace 或 user-global roots，沿用团队已经熟悉的管理方式。",
      },
      {
        title: "不重启刷新",
        desc: "打开 auto-detect 与 auto-refresh，让兼容目录里的修改无需重启运行时就能生效。",
      },
      {
        title: "沿原生态继续分发",
        desc: "通过仓库或 ClawHub 兼容 bundle 来分享，而不是为了一个新能力去 fork 整个 HopClaw。",
      },
    ],
    note: "今天最强的已发布迁移路径，是本地与 ClawHub 兼容的 skill 复用加热刷新；更广的外部 plugin host 仍在单独演进。",
  },
};
