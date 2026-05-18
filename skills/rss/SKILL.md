---
name: rss
description: Retrieve and summarize RSS or Atom feeds.
user-invocable: true
metadata:
  openclaw:
    skillKey: util.rss
    emoji: "\U0001F4F0"
    always: false
---
# RSS

Use current web capabilities to retrieve and summarize RSS or Atom feeds.

Preferred approach:

- If the user provides a feed URL, fetch that feed directly.
- If the user provides a site but not the feed URL, use web search to find the official feed before fetching it.

Return feed items as a concise list containing:

- title
- link
- publication date when available
- brief summary

Working rules:

- Be explicit about which feed URL you used.
- Do not spider broadly or guess many feed URLs. Prefer official, discoverable feeds.
- If the URL is not actually a feed, explain that clearly and suggest the likely next step.
