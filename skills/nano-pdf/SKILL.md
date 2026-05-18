---
name: nano-pdf
description: Inspect, extract, and transform PDF files using existing runtime capabilities.
user-invocable: true
command-dispatch: tool
command-tool: nano-pdf.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: util.nano-pdf
    emoji: "\U0001F4C4"
    always: false
---
# Nano PDF

Use existing runtime capabilities to inspect PDFs, extract text, and perform safe PDF transformations. Prefer the dedicated `nano-pdf.run` tool when it is available in this turn.

Preferred approach:

- Use `nano-pdf.run` for text extraction, page inspection, merging, splitting, and other PDF-aware operations.
- Use existing workspace and file capabilities already available in the turn to locate input files and place generated outputs where the user expects them.
- If the current tool list truly lacks the needed PDF capability, use `skill.ensure` before reaching for local shell utilities.

Working rules:

- Preserve source files unless the user explicitly asks to overwrite them.
- Call out when a PDF appears scanned, image-only, password-protected, or otherwise likely to need OCR or special handling.
- Be precise about page ranges, output filenames, and whether the user wants extracted text versus structural manipulation.
- Never expose sensitive document content beyond the requested scope.
- Do not teach `pdftotext`, `python3`, or ad hoc shell workflows when existing capabilities can complete the task.
