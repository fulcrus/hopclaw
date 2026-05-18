# Quick Start

## TL;DR

- Fastest path: install and jump straight into `hopclaw onboard --web-first`
- Manual path: copy `config.example.yaml`, export model credentials, then run `hopclaw serve`
- Confirm the runtime with `curl /healthz`, `hopclaw dashboard --open`, and `hopclaw doctor`

English is canonical in this file. 中文同步 follows after the English section.

## Fastest Web-First Path

On macOS or Linux:

```bash
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
hopclaw dashboard --open
```

On Windows PowerShell:

```powershell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'
irm https://hopclaw.com/install.ps1 | iex
hopclaw dashboard --open
```

This path writes a minimal config, starts the local gateway, and lets you finish model and channel setup in the dashboard.

## Manual Five-Minute Path

1. Copy the example config.

```bash
cp config.example.yaml local.yaml
```

2. Export the minimum runtime variables.

```bash
export HOPCLAW_AUTH_TOKEN=change-me
export OPENAI_BASE_URL=https://api.openai.com/v1
export OPENAI_API_KEY=your-api-key
export OPENAI_MODEL=gpt-4.1-mini
```

3. Start the gateway.

```bash
hopclaw serve --config ./local.yaml
```

4. Verify the runtime surface.

```bash
curl http://127.0.0.1:16280/healthz
curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  http://127.0.0.1:16280/runtime/tools
```

5. Open the operator console.

```bash
hopclaw dashboard --open
```

## Recommended First Checks

Run these once the gateway is up:

```bash
hopclaw doctor
hopclaw models list
hopclaw skills list
```

## What To Read Next

- `docs/getting-started/first-conversation.md`
- `docs/getting-started/configuration.md`
- `docs/troubleshooting/doctor.md`

## 中文同步

### TL;DR

- 最快方式：安装后直接进入 `hopclaw onboard --web-first`
- 手动方式：复制 `config.example.yaml`、导出模型凭证、执行 `hopclaw serve`
- 启动后用 `curl /healthz`、`hopclaw dashboard --open`、`hopclaw doctor` 做首轮确认

### 推荐流程

- 想最快跑起来：直接走上文 `Fastest Web-First Path`
- 想显式控制配置文件：按上文 `Manual Five-Minute Path` 逐步执行
- 跑起来后：立刻执行上文 `Recommended First Checks`

