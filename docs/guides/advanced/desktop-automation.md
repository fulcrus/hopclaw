# Desktop Automation

## TL;DR

- Desktop automation depends on the local desktop helper, not just the main gateway process.
- Pair and launch the helper with `hopclaw devices pair desktopd` and `hopclaw devices launch desktopd`.
- Confirm what your runtime exposes with `hopclaw tools search desktop` and `hopclaw tools search nodes`.

English is canonical in this file. 中文同步 follows after the English section.

## Pair The Desktop Helper

Create a pairing code:

```bash
hopclaw devices pair desktopd
```

Launch the helper with the printed pairing code:

```bash
hopclaw devices launch desktopd \
  --gateway-url http://127.0.0.1:16280 \
  --pairing-code <PAIRING_CODE>
```

## Verify Exposure

```bash
hopclaw tools search desktop
hopclaw tools search nodes
hopclaw tools info nodes.screen_capture
```

Depending on your helper and bridge configuration, the live runtime may expose `desktop.*`, `nodes.*`, or both.

## Common Desktop Surfaces

- screen capture and recording
- clipboard read/write
- process and system inspection
- notifications
- local app or URL open
- environment and device status

## Practical First Check

After the helper is paired, verify that at least one desktop-related tool is visible:

```bash
hopclaw tools search screen
hopclaw tools search clipboard
```

If nothing appears, fix pairing or helper health first. Do not debug prompts before that.

## 中文同步

### TL;DR

- 桌面自动化依赖 `desktopd` helper，不是只启动 gateway 就够
- 先 `hopclaw devices pair desktopd`，再 `hopclaw devices launch desktopd`
- 用 `hopclaw tools search desktop` 和 `hopclaw tools search nodes` 看实际暴露了哪些能力

### 常用命令

```bash
hopclaw devices pair desktopd
hopclaw devices launch desktopd --gateway-url http://127.0.0.1:16280 --pairing-code <PAIRING_CODE>
hopclaw tools search desktop
hopclaw tools search nodes
```
