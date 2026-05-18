## Summary

Describe the change in 1-3 short paragraphs or bullets.

## Why

- What user or operator problem does this solve?
- Why is this the right place to solve it?

## Testing

- [ ] `gofmt -w ./...`
- [ ] `go vet ./...`
- [ ] `go test ./...`

List any focused commands, manual checks, or screenshots below.

## Risk

- Does this change any CLI, config, API, or release behavior?
- Does it affect security, approvals, update flows, or persistence?

## Docs

- [ ] README updated if user-facing behavior changed
- [ ] `config.example.yaml` updated if config keys changed
- [ ] docs updated if workflow or runtime semantics changed

## Checklist

- [ ] The change is scoped and coherent
- [ ] Tests were added or updated where behavior changed
- [ ] I did not bundle unrelated cleanup into this PR
