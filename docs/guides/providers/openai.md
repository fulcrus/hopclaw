# OpenAI Provider

## TL;DR

- Use `hopclaw setup` for the shortest OpenAI path, or define `models.providers.openai` by hand.
- Set `OPENAI_API_KEY`, then validate with `hopclaw models validate openai`.
- With the current config builder, `agent.default_model` can stay as raw OpenAI model IDs such as `gpt-4o` or `gpt-4.1`.

English is canonical in this file. 中文同步 follows after the English section.

## Fastest Setup

If you want the guided flow:

```bash
hopclaw setup
```

If you prefer environment-first setup:

```bash
export OPENAI_API_KEY="sk-..."
hopclaw serve
```

## Minimal YAML

```yaml
agent:
  default_model: "gpt-4o"

models:
  default_provider: openai
  providers:
    openai:
      api: openai-completions
      base_url: "https://api.openai.com/v1"
      api_key: env:OPENAI_API_KEY
      default_model: "gpt-4o"
```

Switch to the newer Responses API if your deployment standardizes on it:

```yaml
models:
  default_provider: openai
  providers:
    openai:
      api: openai-responses
      base_url: "https://api.openai.com/v1"
      api_key: env:OPENAI_API_KEY
      default_model: "gpt-4.1"
```

## Validation Loop

```bash
hopclaw config validate
hopclaw models list
hopclaw models validate openai
hopclaw doctor auth
hopclaw doctor connectivity
```

## Common Notes

- `openai` is one of the providers that can use an unqualified `agent.default_model`.
- If you need extra headers or a key pool, use the named provider block instead of relying only on environment variables.
- If you point OpenAI traffic at a gateway or proxy, keep the provider name `openai` only when the upstream is truly OpenAI-compatible.

## 中文同步

### TL;DR

- 最快方式是跑 `hopclaw setup`，或者手动写 `models.providers.openai`
- 设置 `OPENAI_API_KEY` 后，用 `hopclaw models validate openai` 校验
- 当前配置生成器下，`agent.default_model` 可直接写 `gpt-4o`、`gpt-4.1`

### 最小配置

见上文 `Minimal YAML`，通常只需要 `base_url`、`api_key`、`default_model`。

### 校验命令

```bash
hopclaw config validate
hopclaw models validate openai
hopclaw doctor auth
hopclaw doctor connectivity
```
