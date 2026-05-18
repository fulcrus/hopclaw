---
name: blogwatcher
description: Monitor blog feeds and summarize updates using existing runtime capabilities.
user-invocable: true
command-dispatch: tool
command-tool: blogwatcher.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: util.blogwatcher
    emoji: "\U0001F4F0"
    always: false
---
# Blog Watcher

Use existing runtime capabilities to monitor RSS/Atom feeds and summarize new posts. Prefer the dedicated `blogwatcher.run` tool when it is available in this turn.

Preferred approach:

- If the user provides a feed URL, use `blogwatcher.run` or the current feed capability directly.
- If the user provides a site but not the feed URL, use `search.web` to find the official feed before monitoring it.
- If the current tool list truly lacks the needed feed capability, use `skill.ensure` before falling back to generic fetch tools.

Working rules:

- Be explicit about which feed URL is being monitored or summarized.
- Return items with title, link, publication date when available, and a concise summary.
- Distinguish between “feed unavailable”, “no new posts”, and “site page is not a feed”.
- Respect publisher rate limits and avoid broad feed guessing or unnecessary polling.
- Do not teach `curl`, XML one-liners, or ad hoc parsing scripts when existing capabilities can complete the task.
