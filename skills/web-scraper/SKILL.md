---
name: web-scraper
description: Fetch and extract content from web pages using existing web and browser capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: util.web-scraper
    emoji: "\U0001F578\uFE0F"
    always: false
---
# Web Scraper

Use existing runtime capabilities to fetch and extract web content:

- `search.web` to discover relevant pages when the user did not provide a URL
- `net.fetch` to retrieve a known URL
- browser tools when the page is JavaScript-rendered, login-gated, or needs interaction

Working rules:

- Prefer the simplest capability that can reach the content reliably.
- Return the extracted result together with the source URL and any important limitations, such as partial extraction or JavaScript-only content.
- Respect robots.txt, rate limits, and authentication boundaries. Do not attempt to bypass them.
- Do not fall back to `curl`, `wget`, or one-off parsing scripts when the runtime already has a direct capability.
