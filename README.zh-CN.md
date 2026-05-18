# HopClaw

[English README](./README.md)

`hopclaw` 是一个本地优先的 agent 运行时，带有 operator 控制台、频道桥接和
HTTP 控制 API。当前仓库里已经落地的内容包括：agent 主循环、run/session
存储与队列、工具执行、审批、artifact、审计与事件流、约 23 个 channel
适配层、skill 加载器、capability 扩展点（plugin、MCP、bundle）、自动化面
（watch、cron、wakeup、automation intent、durable facts、knowledge 源）、
以及支撑控制台的 control-plane / governance delivery / quality / eval-suite
等表面。

## 适合谁

HopClaw 适合那些想把 Agent 真正带进现实任务里的个人和团队：

- 个人开发者、研究者、重度电脑用户，希望把浏览器、桌面、文件、命令和可回查结果收进一套本地 runtime
- 需要把 agent 接进 Slack、飞书、Discord、Telegram 或 webhook，并要求审批与审计
- 已经有内部系统或调度器，希望通过 HTTP 驱动 runs，而不是把聊天当成唯一控制面
- 需要 browser / desktop 自动化，但不想把宿主相关逻辑硬塞进核心 runtime
- 正在把已有 `SKILL.md` 或 `.openclaw` 资产迁进更严格运行面的人

## Runtime 概念

- `session_key` 是对话、流程与产品入口的主隔离键。
- `automation_id` 是可选的标签，用于标记某个自动化或流程家族。
- HopClaw 是单实例运行时。没有内建的多租户、组织结构或角色模型。如果你需要这些，请在外层系统里包一层，通过 HTTP API 和 AuthZ decider 契约与 HopClaw 交互。

## 当前能力

仓库当前包含：

- session / run 存储与队列协调
- 上下文压缩与 model router
- OpenAI-compatible 模型接入
- 内置 file / exec / net / text / runtime 工具
- 内置 office 工具：文档、表格、演示文稿、ICS 日历与 CalDAV 日历
- git、packages、containers、search、speech、media、media generation、vision、email.send 等 Layer 2 工具组
- 审批流，带 grant store 与 timeout sweeper
- artifact 存储、审计、事件流
- governance delivery adapters 与 reliable dispatcher
- quality summary、release readiness、eval suite 执行（HTTP 与 CLI）
- 带发布渠道的更新检查，以及本地 bug report 打包能力
- `SKILL.md` 加载、绑定，以及来自本地与远程目录的 skill 发现
- `skill.ensure` 按运行时策略自动继续或审批门控安装缺失 skill
- plugin 系统，把 process-backed 模块投射进 runtime module 目录
- MCP client / server / bridge / manager，通过 Model Context Protocol 扩展工具
- capability bundle（当前只有 `bundles/feishu-suite`）
- 自动化面：`watch` 源、`cron` 定时、`wakeup` 唤醒、automation intent 分类、durable facts、knowledge 源
- HTTP 端点覆盖 runs、approvals、artifacts、tools、events、quality、evals、governance delivery、knowledge、watch、cron、wakeup、automation、module catalog
- 内建 channel adapters，包括 Feishu、Slack、Discord、Telegram、WhatsApp、Signal、Google Chat、LINE、Microsoft Teams、IRC、Matrix、Mattermost、Nextcloud Talk、BlueBubbles、iMessage（legacy bridge）、Nostr、Synology Chat、Tlon、Twitch、Zalo、Zalo Personal，以及 stdio/plugin channel 和 webhook 风格集成
- 内建 channel 工具：`channel.list`、`channel.status`、`channel.send`、`channel.edit`、`channel.delete`、`channel.react`、`channel.history`、`channel.action`
- 本地 operator 控制台，由同一 HTTP 表面驱动，支持英 / 简中 / 繁中 / 日语
- 仓库内置了一批本地 skill，可补充 Notion、Jira、Trello、Apple Notes、Apple Reminders、邮件工作流、Slack 工作流等效率场景
- capability 注册与 `browser.*` 工具（可内建 helper，也可连接外部 `hopclaw-browserd`）
- `desktop.*` 工具（可内建 helper，也可连接外部 `hopclaw-desktopd`）
- 外部 helper 的 device pairing 与 auth

## 渠道与办公能力

HopClaw 把内置能力分为：

- `supported`：属于主产品契约，有目录可见性与主路径文档
- `experimental`：代码已在仓内，但 onboarding、测试与 operator 指引的深度更薄

### 渠道

- 消息渠道：Feishu、Slack、Discord、Telegram、WhatsApp、Signal、Google Chat、LINE、Microsoft Teams、IRC、Matrix、Mattermost、Nextcloud Talk
- 特殊或个人渠道：BlueBubbles、iMessage（legacy bridge）、Nostr、Synology Chat、Tlon、Twitch、Zalo、Zalo Personal
- 本地与集成面：WebChat、stdio/plugin channels、webhook bridges
- 通用渠道操作：列渠道、看状态、发消息、编辑、删除、加/删 reaction、读历史、自定义 action

### 办公与效率

- office 文件工具：`document.*`、`spreadsheet.*`、`presentation.*`
- 日历工具：`calendar.create_ics`、`calendar.parse_ics`、`calendar.list_events`、`calendar.create_event`、`calendar.update_event`、`calendar.delete_event`
- 邮件工具：`email.send`、`email.list`、`email.read`、`email.search`、`email.download_attachment`
- Feishu/Lark 产品 bundle：通过 `bundles/feishu-suite` 提供 docs、wiki、drive、bitable、URL 解析
- 仓库内置的效率 skill：Notion、Jira、Trello、Apple Notes、Apple Reminders、Things、Bear Notes、Slack、email 等

这些能力里，一部分是 runtime 内置工具，一部分是 skill 或 bundle 形式的集成；后者通常依赖外部凭证、CLI 或第三方服务配置。

## 本地安装

已打 tag 的版本现在会自动产出 Linux、macOS、Windows 的 `amd64` / `arm64` 二进制压缩包。每个平台压缩包会同时带上该平台对应的 CLI 和 helper 二进制，以及 `README`、`CHANGELOG`、`SECURITY`、`LICENSE` 和 `NOTICE`。

如果你想直接用一键安装脚本拉取最新 release：

```sh
curl -fsSL https://hopclaw.com/install.sh | sh
```

安装脚本会在 `/usr/local/bin` 已可写时直接装到那里；如果当前用户对该目录没有写权限，就自动退回到 `~/.local/bin`，避免首次安装就要求 `sudo`。

如果你在 Windows PowerShell 上安装最新 release：

```powershell
irm https://hopclaw.com/install.ps1 | iex
```

如果你想在 macOS / Linux 上直接安装，并走 web-first 的本地控制台启动路径：

```sh
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
```

这会运行 `hopclaw onboard --web-first`：先写入最小本地配置、启动 gateway、打开 dashboard，把 models / channels 留到 web 控制台里再配。

如果你想在 Windows PowerShell 上直接安装，并走同样的 web-first 路径：

```powershell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex
```

常用安装脚本变量：

- `HOPCLAW_INSTALL_BINARY=all`：从同一个 release bundle 一次装上 CLI、`openclaw` 兼容别名和 helper 二进制
- `HOPCLAW_INSTALL_BINARY=hopclaw-browserd`：安装 helper，而不是主 CLI
- `HOPCLAW_INSTALL_VERSION=2026.3.17`：安装指定版本
- `HOPCLAW_INSTALL_DIR=...`：修改安装目录
- `HOPCLAW_INSTALL_REPO=owner/repo`：切到镜像仓库或其他 release 源
- `HOPCLAW_INSTALL_BASE_URL=https://mirror.example.com/releases`：把安装脚本切到镜像的 hosted release 地址
- `HOPCLAW_INSTALL_RUN_ONBOARD=1`：安装完成后直接启动 `hopclaw onboard --web-first`

如果你更想从源码安装：

```sh
go install github.com/fulcrus/hopclaw/cmd/hopclaw@latest
```

如果你本地还想沿用旧命名 `openclaw`：

```sh
go install github.com/fulcrus/hopclaw/cmd/openclaw@latest
```

如果你要启用独立 browser host：

```sh
go install github.com/fulcrus/hopclaw/cmd/hopclaw-browserd@latest
```

如果你要启用独立 desktop host：

```sh
go install github.com/fulcrus/hopclaw/cmd/hopclaw-desktopd@latest
```

这两个命令都会启动同一个 runtime，只是本地二进制名不同。

tag 发布时会同步推送多架构容器镜像到 GHCR。

Homebrew 现在可以先通过仓库内置的 `HEAD` formula 启动：

```sh
brew tap fulcrus/hopclaw https://github.com/fulcrus/hopclaw
brew install --HEAD fulcrus/hopclaw
```

后续如果拆出独立 bottle/tap 仓库，也可以继续复用同一套 release 产物，不影响安装入口。

## 快速开始

在 macOS / Linux 上最快的路径：

```sh
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
```

这会安装最新 release，并直接进入引导式 `hopclaw onboard`。

在 Windows PowerShell 上最快的路径：

```powershell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex
```

这会安装最新 release、把用户级安装目录加入 `PATH`，并直接进入同一套 `hopclaw onboard` 引导。

如果你想手动走完整路径：

1. 复制示例配置：

```sh
cp config.example.yaml local.yaml
```

2. 设置环境变量：

```sh
export HOPCLAW_AUTH_TOKEN=change-me
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4.1-mini
```

也可以直接使用 `hopclaw setup` 或 `hopclaw onboard`，让 HopClaw 自动识别
`DEEPSEEK_API_KEY`、`DASHSCOPE_API_KEY`、`MOONSHOT_API_KEY`、`MINIMAX_API_KEY`、
`XIAOMI_API_KEY`、`QIANFAN_API_KEY`、`ZAI_API_KEY`、`VOLCENGINE_API_KEY`、
`HUNYUAN_API_KEY`、`SILICONFLOW_API_KEY` 等国内外主流模型平台密钥。

3. 启动 gateway：

```sh
hopclaw serve --config ./local.yaml
```

4. 验证服务已经启动，并查看当前能力表面：

```sh
curl http://127.0.0.1:16280/healthz
curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" http://127.0.0.1:16280/runtime/tools
```

5. 打开本地控制台：

```sh
hopclaw dashboard
```

推荐的本地桌面运行配置：

```yaml
runtime:
  profile: desktop
  status_reminder_delay: 6s
  audit:
    enabled: true
skills:
  install_policy: ask
```

推荐的发布治理配置：

```yaml
update:
  enabled: true
  check_on_start: true
  channel: stable
  manifest_url: https://hopclaw.com/releases/manifest.json
diagnostics:
  enabled: true
  bug_report_dir: ./.hopclaw/reports
  include_logs: true
  telemetry_enabled: false
  # telemetry_endpoint: https://telemetry.example.com/api/v1/ingest/events
  # telemetry_token: env:HOPCLAW_TELEMETRY_TOKEN
  crash_reports_enabled: false
```

telemetry 建议：

- 严格 toB / 私有化场景默认保持 `telemetry_enabled: false`
- 需要出站上报时，把 `telemetry_endpoint` 指向你自己维护或明确信任的采集端点
- 需要匿名安装 / 活跃 / plugin / skill 统计时，再显式配置 `telemetry_endpoint` 和 `telemetry_token`
- 需要把数据留在自己边界内时，在 gateway 侧开启 `telemetry_collector_enabled`
- telemetry 默认是 best-effort 且静默失败；只有显式打开 `diagnostics.telemetry_debug_log: true` 才会输出调试日志
- 这些指标应理解为安装量和活跃安装量，而不是精确“用户数”

skill 安装策略：

- `ask`：agent 调用 `skill.ensure` / `skill.install` 时先创建审批，用户确认后继续执行
- `auto`：自动安装并继续执行，不暂停 run
- `deny`：禁止运行时安装 skill，agent 需要明确说明当前缺少的能力

问题反馈与版本可见性：

- `hopclaw bug-report` 会生成一个本地脱敏 ZIP 包，便于手动提交 issue
- `hopclaw update --check` 会按当前 channel 检查可见的新版本
- `hopclaw doctor` 会展示最近一次更新检查结果

首页现在支持中英文切换，并提供本地 operator console 骨架。它会展示 runtime 状态、capabilities、runs、审批、sessions 和最近事件；更深的控制面仍然依赖底层 HTTP API。

## Capability Hosts

- HopClaw 可以通过 gateway 注册外部 capability host。
- 当前第一个落地的 host 插槽是 `hosts.browser`，用于连接外部 browser daemon。
- 当前还支持 `hosts.desktop`，用于连接外部 desktop automation daemon。
- 配置后，browser host 会出现在 `GET /operator/capabilities` 和本地控制台中。
- 配置后，runtime 也会暴露 `browser.create_session`、`browser.navigate`、`browser.click`、`browser.type`、`browser.wait_for`、`browser.snapshot`、`browser.screenshot`、`browser.list_tabs`。
- 配置后，runtime 也会暴露 `desktop.create_session`、`desktop.open_app`、`desktop.focus_app`、`desktop.focus_window`、`desktop.list_apps`、`desktop.list_windows`、`desktop.type_text`、`desktop.hotkey`、`desktop.screenshot`、`desktop.capture_tree`、`desktop.clipboard_read`、`desktop.clipboard_write`。

可选的 browser host 最小启动方式：

```sh
export HOPCLAW_BROWSER_TOKEN=change-me-browser
hopclaw-browserd -listen 127.0.0.1:9223 -auth-token "$HOPCLAW_BROWSER_TOKEN"
```

然后在 `local.yaml` 里打开：

```yaml
hosts:
  browser:
    enabled: true
    base_url: http://127.0.0.1:9223
    auth_token: ${HOPCLAW_BROWSER_TOKEN}
```

可选的 desktop host 最小启动方式：

```sh
export HOPCLAW_DESKTOP_TOKEN=change-me-desktop
hopclaw-desktopd -listen 127.0.0.1:9224 -auth-token "$HOPCLAW_DESKTOP_TOKEN"
```

然后在 `local.yaml` 里打开：

```yaml
hosts:
  desktop:
    enabled: true
    base_url: http://127.0.0.1:9224
    auth_token: ${HOPCLAW_DESKTOP_TOKEN}
```

## 最小 API 示例

查看工具清单：

```sh
curl -s \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  http://127.0.0.1:16280/runtime/tools
```

创建并执行一个 run：

```sh
curl -s \
  -X POST \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  http://127.0.0.1:16280/runtime/runs \
  -d '{
    "session_key": "demo-session",
    "content": "列出当前工作目录的文件，并概括项目结构。"
  }'
```

## Runtime 档位

- `runtime.profile: desktop` 是默认值，保持本地优先行为，同时对写类工具继续要求审批。
- `runtime.profile: trusted_desktop` 适合可信的个人桌面环境，会降低一部分审批摩擦，但不会放开 destructive 工具的保护。
- `runtime.profile: production` 会收紧默认值：默认 `exec.mode=allowlist`、默认 `net.allow_local=false`、策略层拒绝 destructive 工具，并强制要求 `server.auth_token`、`runtime.audit.enabled: true` 和持久化 store backend。
- `runtime.status_reminder_delay` 控制 channel bridge 在任务长时间未完成时，多久向终端用户发送“仍在处理中”的提示。

## 安全与文档入口

- 默认监听地址仍是 `127.0.0.1:16280`，对外暴露前应配置 `server.auth_token` 和额外访问控制层。
- 网络工具默认 `allow_private: false`，避免直接访问内网地址。
- 安全漏洞不要走公开 issue，请按 [`SECURITY.md`](./SECURITY.md) 里的流程私下上报。
- 版本变更记录见 [`CHANGELOG.md`](./CHANGELOG.md)。
- Runtime API 的 OpenAPI / Swagger 文档见 [`docs/openapi/runtime-v1.yaml`](./docs/openapi/runtime-v1.yaml)。
- 如果要本地预览 Swagger UI 或 Redoc，见 [`docs/openapi/README.md`](./docs/openapi/README.md)。

## 目录结构

- [`agent/`](./agent)：agent 主循环、存储、队列
- [`approval/`](./approval)：审批 ticket、grant 与 timeout sweeper
- [`artifact/`](./artifact)：artifact 存储与预览
- [`audit/`](./audit)：审计 sink 与可靠投递
- [`authz/`](./authz)：AuthZ decider 契约
- [`bootstrap/`](./bootstrap)：应用装配
- [`bundles/`](./bundles)：capability bundle（当前只有 `feishu-suite`）
- [`capabilities/`](./capabilities)：具体 capability 适配层
- [`capability/`](./capability)：capability manifest、health 与 registry
- [`channels/`](./channels)：channel 适配层与 bridge
- [`cmd/`](./cmd)：入口二进制（`hopclaw`、`hopclaw-browserd`、`hopclaw-desktopd`、`hopclaw-gateway`、`openclaw`）
- [`config/`](./config)：YAML 配置、校验与 product catalog
- [`contextengine/`](./contextengine)：上下文准备
- [`controlplane/`](./controlplane)：控制面状态探针与共享类型
- [`cron/`](./cron) / [`watch/`](./watch) / [`wakeup/`](./wakeup)：自动化调度与监听
- [`eventbus/`](./eventbus)：事件总线与 typed payload
- [`gateway/`](./gateway)：operator API 与本地控制台
- [`internal/browserd/`](./internal/browserd) / [`internal/desktopd/`](./internal/desktopd)：独立 host 实现
- [`knowledge/`](./knowledge)：knowledge 源
- [`mcp/`](./mcp)：MCP client / server / bridge / manager
- [`plugin/`](./plugin)：plugin 加载器、安装器与 manager
- [`policy/`](./policy)：策略引擎与 grant wiring
- [`runtime/`](./runtime)：runtime service 层
- [`server/`](./server)：runtime HTTP API
- [`skill/`](./skill)：skill 加载与注册
- [`store/`](./store)：持久化后端
- [`toolruntime/`](./toolruntime)：内置工具与 Layer 2 工具组

## 文档

- [`docs/`](./docs)：安装、配置、渠道与提供商集成、运维 runbook、排障与参考资料
- [`docs/openapi/runtime-v1.yaml`](./docs/openapi/runtime-v1.yaml)：Runtime API 的 OpenAPI 3.1 描述
- [`examples/`](./examples)：可直接复制的参考模板，覆盖 skill、hooks、webhook 集成、stdio channel plugin、external HTTP tool 与 capability host
- [`VERSIONING.md`](./VERSIONING.md)：版本、发布渠道、兼容性与废弃策略
- [`CHANGELOG.md`](./CHANGELOG.md)：版本化变更记录
- [`SECURITY.md`](./SECURITY.md)：漏洞上报流程

## License

HopClaw 使用 MIT License。见 [`LICENSE`](./LICENSE) 与 [`NOTICE`](./NOTICE)。

如果你分发修改版，请保留 license 与 notice 文件、保留来源声明。推荐表述包括 `Based on HopClaw`、`Forked from HopClaw` 或 `Modified from HopClaw`。
