---
name: oracle
description: Fetch, extract, and analyze web content using existing browser and network capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: util.oracle
    emoji: "\U0001F52E"
    always: false
---
# Oracle

Use existing runtime capabilities to inspect pages, extract data, and analyze web content.

Preferred approach:

- If the user did not provide a URL, use `search.web` to discover the correct page first.
- Use `net.fetch` for direct retrieval of a known URL.
- Use browser tools when the page depends on JavaScript, login state, screenshots, or interaction.
- Use structured extraction tools already available in the turn instead of one-off parsing scripts.
- If the current tool list truly lacks the needed web capability, recover it with `skill.ensure` before falling back to raw shell commands.

Working rules:

- Return the source URL with extracted results and mention any important limitations such as truncation, missing sections, or render-only content.
- Preserve useful structure such as headings, tables, links, forms, and metadata when it matters to the task.
- Do not suggest `curl`, `wget`, or ad hoc `python3` parsing when an existing product capability can complete the job.
- Respect authentication boundaries, robots.txt, and rate limits.
