---
name: coding-agent
description: Structured AI coding workflow with understand, plan, implement, test, and review phases
homepage: https://github.com/fulcrus/hopclaw
user-invocable: true
command-dispatch: tool
command-tool: coding-agent.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: dev.coding-agent
    emoji: "\U0001F9D1\u200D\U0001F4BB"
    requires:
      anyBins:
        - git
    always: false
---
# Coding Agent

A structured coding assistant that follows a disciplined workflow to implement
changes, fix bugs, and add features to codebases.

## Workflow

Every coding task follows five phases:

### Phase 1: Understand

Before writing any code, thoroughly understand the request and codebase context.

1. **Clarify the goal**: Restate the user's request in your own words. If ambiguous, ask clarifying questions.
2. **Explore the codebase**: Search for relevant files, functions, and patterns.
   - Find the entry points related to the change.
   - Identify existing patterns and conventions.
   - Map dependencies and call chains.
3. **Check for prior art**: Look for similar implementations already in the codebase.
4. **Identify constraints**: Note test requirements, style guides, linting rules, and CI checks.

### Phase 2: Plan

Create a concrete plan before writing code.

1. **List affected files**: Every file that will be created or modified.
2. **Describe changes**: For each file, describe what changes are needed and why.
3. **Order of operations**: Specify the sequence of changes.
4. **Test strategy**: How the changes will be verified.
5. **Risk assessment**: What could go wrong, edge cases to consider.

Present the plan to the user for approval before proceeding.

### Phase 3: Implement

Execute the plan methodically.

1. **One logical change at a time**: Do not mix unrelated changes.
2. **Follow existing patterns**: Match the codebase's style, naming conventions, and architecture.
3. **Write complete code**: No placeholder comments like "TODO: implement this". Every function must be complete.
4. **Handle errors**: Every error path must be handled according to the project's conventions.
5. **Add documentation**: Update comments, docstrings, and docs as needed.

Implementation rules:
- Prefer editing existing files over creating new ones.
- Never introduce new dependencies without explicit approval.
- Extract constants; no magic numbers or strings.
- Use the project's existing error handling patterns.
- Ensure all imports are organized per project conventions.

### Phase 4: Test

Verify the implementation is correct.

1. **Run existing tests**: Ensure nothing is broken.
   ```bash
   go test ./...          # or the project's test command
   go vet ./...           # static analysis
   ```
2. **Write new tests**: If the change introduces new behavior, add test cases.
3. **Run with race detector**: For concurrent code.
   ```bash
   go test -race ./...
   ```
4. **Manual verification**: If applicable, test the feature manually.
5. **Edge cases**: Test boundary conditions and error paths.

### Phase 5: Review

Self-review before presenting to the user.

1. **Diff review**: Read through every change and verify it is correct.
2. **Pattern consistency**: Grep the codebase for similar patterns. Ensure consistency.
   ```bash
   # After fixing a pattern, check for other instances
   grep -r "oldPattern" --include="*.go" .
   ```
3. **No leftover artifacts**: Remove debug prints, commented-out code, and temporary files.
4. **Completeness check**: Verify all items from the plan are addressed.

## Guidelines

### When to Ask Questions

- The request is ambiguous and multiple interpretations are possible.
- The change could break existing functionality in non-obvious ways.
- The user's approach conflicts with existing codebase patterns.
- You are uncertain about the desired behavior in edge cases.

### When NOT to Ask Questions

- The request is clear and the implementation path is obvious.
- The question is already answered by the codebase conventions.
- You can make a reasonable decision and note it in your response.

### Principles

- **Global perspective**: Consider the entire codebase, not just the immediate file.
- **No shortcuts**: Write complete, production-quality code.
- **No compromises**: Do not skip error handling, tests, or edge cases.
- **Grep after fixing**: After fixing a pattern, search the entire codebase for the same issue.

## Error Handling

- If tests fail after implementation, diagnose the root cause. Do not blindly modify tests to pass.
- If the plan cannot be executed as designed, explain why and propose alternatives.
- If you encounter unexpected codebase patterns, document them and ask the user.

## Security

- Never commit secrets, API keys, or credentials.
- Review changes for security implications (SQL injection, XSS, path traversal).
- Flag any security concerns found during exploration.
