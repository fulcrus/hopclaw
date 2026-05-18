# Browser Automation

## TL;DR

- Browser automation is exposed both as an operator helper surface and as runtime tools under `browser.*`.
- Use `hopclaw browser status` first. If the helper is unavailable, fix that before debugging selectors or prompts.
- Start with `browser.open`, `browser.snapshot_aria`, `browser.click_aria`, `browser.type_aria`, and `browser.screenshot`.

English is canonical in this file. 中文同步 follows after the English section.

## Operator Checks

```bash
hopclaw browser status
hopclaw browser sessions
```

Open a session from the CLI:

```bash
hopclaw browser open https://example.com
hopclaw browser sessions
hopclaw browser tabs <session-id>
hopclaw browser screenshot <session-id> --output /tmp/example.png
```

## Runtime Tool Discovery

```bash
hopclaw tools search browser
hopclaw tools info browser.open
hopclaw tools info browser.snapshot_aria
```

## Recommended Automation Pattern

For agent-driven web work, prefer:

1. `browser.open`
2. `browser.snapshot_aria`
3. `browser.click_aria` / `browser.type_aria`
4. `browser.wait`
5. `browser.screenshot`

ARIA-driven interaction is usually more stable than raw CSS selectors for UI automation.

## Important Tool Families

- Session lifecycle: `browser.open`, `browser.close`, `browser.tabs`, `browser.tab_new`, `browser.tab_switch`, `browser.tab_close`
- Navigation: `browser.navigate`, `browser.back`, `browser.forward`, `browser.reload`
- Interaction: `browser.click`, `browser.click_aria`, `browser.type`, `browser.type_aria`, `browser.select`, `browser.hover`, `browser.scroll`, `browser.drag`, `browser.upload`
- Inspection: `browser.snapshot`, `browser.snapshot_aria`, `browser.element_text`, `browser.element_attr`, `browser.element_visible`, `browser.console_messages`, `browser.network_requests`, `browser.performance`
- Capture: `browser.screenshot`, `browser.screenshot_labeled`, `browser.pdf`, `browser.download`

## 中文同步

### TL;DR

- 浏览器自动化同时有 operator CLI 和 `browser.*` runtime tools 两层入口
- 先跑 `hopclaw browser status`，确认 helper 可用，再排查页面交互问题
- 推荐优先用 `snapshot_aria` + `click_aria` / `type_aria`

### 常用命令

```bash
hopclaw browser status
hopclaw browser open https://example.com
hopclaw browser tabs <session-id>
hopclaw browser screenshot <session-id> --output /tmp/example.png
hopclaw tools search browser
```
