# Installation

## TL;DR

- Fastest path on macOS/Linux: `curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh`
- Fastest path on Windows PowerShell: `$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex`
- Verify the install with `hopclaw version`, `hopclaw doctor`, and `hopclaw health`

English is canonical in this file. 中文同步 follows after the English section.

## Release Installer

Use the published installer when you want the shortest path from zero to a local HopClaw runtime.

```bash
curl -fsSL https://hopclaw.com/install.sh | sh
```

If you want the installer to immediately open the guided onboarding flow:

```bash
curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh
```

Useful installer overrides:

```bash
HOPCLAW_INSTALL_VERSION=2026.3.17 \
HOPCLAW_INSTALL_DIR="$HOME/.local/bin" \
curl -fsSL https://hopclaw.com/install.sh | sh
```

## Windows PowerShell

```powershell
irm https://hopclaw.com/install.ps1 | iex
```

Or launch onboarding immediately:

```powershell
$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'
irm https://hopclaw.com/install.ps1 | iex
```

## Homebrew HEAD

Use this when you want to install directly from the repository tip on macOS:

```bash
brew tap fulcrus/hopclaw https://github.com/fulcrus/hopclaw
brew install --HEAD fulcrus/hopclaw/hopclaw
```

## Source Build

Use source builds when you want to pin a branch, patch locally, or integrate HopClaw into an internal build pipeline.

```bash
go install github.com/fulcrus/hopclaw/cmd/hopclaw@latest
go install github.com/fulcrus/hopclaw/cmd/openclaw@latest
go install github.com/fulcrus/hopclaw/cmd/hopclaw-browserd@latest
go install github.com/fulcrus/hopclaw/cmd/hopclaw-desktopd@latest
```

## Docker From This Repository

If you prefer a local container build instead of a host install:

```bash
docker build -t hopclaw:local .
docker run --rm \
  -p 16280:16280 \
  -v "$HOME/.hopclaw:/root/.hopclaw" \
  hopclaw:local \
  hopclaw serve
```

## Verify The Install

Run these checks before you continue to onboarding or channel setup:

```bash
hopclaw version
hopclaw doctor
hopclaw health
```

If `hopclaw health` fails because the gateway is not running yet, start it first:

```bash
hopclaw onboard --web-first
```

## 中文同步

### TL;DR

- macOS/Linux 最快安装：`curl -fsSL https://hopclaw.com/install.sh | HOPCLAW_INSTALL_RUN_ONBOARD=1 sh`
- Windows 最快安装：`$env:HOPCLAW_INSTALL_RUN_ONBOARD='1'; irm https://hopclaw.com/install.ps1 | iex`
- 安装后先跑：`hopclaw version`、`hopclaw doctor`、`hopclaw health`

### 常用安装方式

- 发布版安装器：适合绝大多数首次用户，命令见上文 `Release Installer`
- Homebrew：适合 macOS 开发机，命令见上文 `Homebrew HEAD`
- 源码安装：适合需要本地改动或内部构建流水线的团队，命令见上文 `Source Build`
- Docker：适合想把运行环境收进容器的场景，命令见上文 `Docker From This Repository`

### 安装后验证

先执行上文的三条校验命令；如果还没启动 gateway，就运行：

```bash
hopclaw onboard --web-first
```
