---
name: mcporter
description: Convert OpenAPI specs to MCP tool definitions and manage tool imports
user-invocable: true
command-dispatch: tool
command-tool: mcporter.run
command-arg-mode: raw
metadata:
  openclaw:
    skillKey: ops.mcporter
    emoji: "\U0001F50C"
    requires:
      anyBins:
        - npx
    always: false
---
# MCPorter

Convert OpenAPI specifications to MCP (Model Context Protocol) tool definitions and manage tool imports/exports.

## Capabilities

- Convert OpenAPI/Swagger specs to MCP tool definitions
- Import tools from OpenAPI JSON or YAML files
- Export MCP tool definitions to various formats
- Validate MCP tool schemas
- Generate tool stubs from API specifications

## Usage

### Converting OpenAPI to MCP

```bash
# Convert an OpenAPI spec to MCP tools (using a converter package)
npx @anthropic/openapi-to-mcp convert openapi.yaml --output tools.json

# Convert from a URL
npx @anthropic/openapi-to-mcp convert https://api.example.com/openapi.json --output tools.json
```

### Validating Tool Definitions

```bash
# Validate a tool definition file
npx @anthropic/openapi-to-mcp validate tools.json

# Validate with strict mode
npx @anthropic/openapi-to-mcp validate --strict tools.json
```

### Listing Tools

```bash
# List tools in a definition file
npx @anthropic/openapi-to-mcp list tools.json

# List with details
npx @anthropic/openapi-to-mcp list --verbose tools.json
```

### Generating Stubs

```bash
# Generate tool implementation stubs
npx @anthropic/openapi-to-mcp generate --lang go --output ./tools/ openapi.yaml

# Generate with specific operations only
npx @anthropic/openapi-to-mcp generate --operations "getUser,createUser" openapi.yaml
```

## Examples

- `npx @anthropic/openapi-to-mcp convert openapi.yaml --output tools.json`
- `npx @anthropic/openapi-to-mcp validate tools.json`
- `npx @anthropic/openapi-to-mcp list tools.json`

## Error Handling

- If the OpenAPI spec is invalid, the converter will report validation errors.
- Large specs may take time to convert. Be patient with complex APIs.
- npx requires Node.js to be installed. If not available, suggest installation.
- If the package is not found, check npm registry availability.
