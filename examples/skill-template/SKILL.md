---
name: sample-local-workflow
description: Template for a SKILL.md-based workflow that combines agent instructions with a local helper script
homepage: https://example.com/replace-with-your-skill-homepage
user-invocable: true
command-dispatch: tool
command-tool: exec_command
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: examples.sample-local-workflow
    emoji: "[tool]"
    requires:
      anyBins:
        - bash
      env:
        - SAMPLE_API_TOKEN
    always: false
---
# Sample Local Workflow

Use this skill when the user asks for a repeatable workflow that you want to
package as a reusable capability.

## When To Use

- The task is a good fit for a small local helper script.
- The workflow has clear preconditions and validation rules.
- The result should be reproducible and easy to inspect.

## Workflow

1. Confirm the required input from the user.
2. Check prerequisites before running anything:
   - verify required binaries are installed
   - verify required environment variables are present
3. Run the local helper script from this skill directory:

```bash
./scripts/run.sh "<primary-input>"
```

4. Inspect the output before reporting success.
5. If the command fails, explain the failure and the missing prerequisite or
   bad input.

## Expected Output Shape

The helper script in this template prints JSON like:

```json
{
  "ok": true,
  "input": "example",
  "summary": "replace this with your real operation"
}
```

Prefer structured output when the skill will be used often. It is easier for
the agent to validate and summarize.

## Security Notes

- Do not interpolate untrusted user input into shell without validation.
- Do not echo secrets back to the user.
- If the workflow can write, delete, or call external systems, require
  explicit confirmation first.

## How To Adapt This Template

- Replace `SAMPLE_API_TOKEN` with the real credential name.
- Replace `./scripts/run.sh` with your real command or wrapper.
- Add concrete examples of valid input and failure cases.
- If the skill depends on repo-local files, state the exact paths.
