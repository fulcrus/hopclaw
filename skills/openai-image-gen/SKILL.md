---
name: openai-image-gen
description: Generate and edit images using existing image-generation capabilities.
homepage: https://platform.openai.com/docs/guides/images
user-invocable: true
command-dispatch: tool
command-tool: image.generate
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: ai.openai-image-gen
    emoji: "\U0001F3A8"
    primaryEnv: OPENAI_API_KEY
    requires:
      env:
        - OPENAI_API_KEY
    always: false
---
# OpenAI Image Generation

Use existing runtime capabilities to create or edit images. Prefer the builtin `image.generate` tool with `provider=openai` when it is available in this turn.

Preferred approach:

- Use `image.generate` with `provider=openai` for generation and image-conditioned edits.
- Use existing file or artifact capabilities when the user wants the result saved into the workspace.
- If the current tool list truly lacks the needed capability, use `skill.ensure` before inventing raw API calls.

Working rules:

- Clarify prompt, style, size, variation count, and reference-image requirements only when missing details materially change the result.
- Tell the user when a request may hit provider policy or cost constraints.
- If the user wants multiple variants, explain how many outputs are actually supported by the current capability.
- Never expose API keys, signed URLs, or provider secrets in output.
- Do not teach raw HTTP or shell workflows when existing capabilities can complete the task.
