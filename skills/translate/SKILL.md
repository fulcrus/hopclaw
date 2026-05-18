---
name: translate
description: Translate text between languages using available translation capabilities.
user-invocable: true
metadata:
  openclaw:
    skillKey: util.translate
    emoji: "\U0001F310"
    always: false
---
# Translate

Use the best translation capability already available in the runtime.

Preferred order:

- Use a dedicated translation capability when one is already available.
- Otherwise use an existing verified external translation path if the runtime is configured for it.
- For short, low-risk text when no verified translation capability is available, you may translate directly with your multilingual knowledge, but say clearly that the result is model-generated rather than tool-verified.

Working rules:

- Preserve the user's formatting, lists, and code blocks unless they asked for a rewrite.
- Ask only when the target language, tone, or register is missing and materially changes the result.
- For long text, split at paragraph or sentence boundaries and keep terminology consistent across chunks.
- Never expose API keys or credentials in output.
