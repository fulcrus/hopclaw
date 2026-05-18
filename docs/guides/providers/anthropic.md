# Anthropic Provider

## TL;DR

- Configure Anthropic under `models.providers.anthropic`.
- Set `ANTHROPIC_API_KEY`, then validate with `hopclaw models validate anthropic`.
- For non-OpenAI-compatible providers such as Anthropic, the runtime model is typically qualified as `anthropic/<model>`.

English is canonical in this file. 中文同步 follows after the English section.

## Minimal YAML

```yaml
agent:
  default_model: "anthropic/claude-sonnet-4-20250514"

models:
  default_provider: anthropic
  providers:
    anthropic:
      api: anthropic-messages
      base_url: "https://api.anthropic.com"
      api_key: env:ANTHROPIC_API_KEY
      default_model: "claude-sonnet-4-20250514"
```

## Validation Loop

```bash
hopclaw config validate
hopclaw models list
hopclaw models validate anthropic
hopclaw doctor auth
hopclaw doctor connectivity
```

## Notes

- The built-in provider catalog treats Anthropic as an `anthropic-messages` API surface, not OpenAI-compatible chat/completions.
- If you use a proxy that imitates Anthropic, keep the same layout but override `base_url`.
- If you want multiple credentials or custom headers, add them in the provider block instead of hard-coding them into prompts or wrappers.

## 中文同步

### TL;DR

- Anthropic 应配置在 `models.providers.anthropic`
- 设置 `ANTHROPIC_API_KEY` 后用 `hopclaw models validate anthropic` 校验
- 运行时默认模型一般写成 `anthropic/<model>`

### 最小配置

上文 YAML 已包含当前最小可用字段：`api`、`base_url`、`api_key`、`default_model`。

### 校验命令

```bash
hopclaw config validate
hopclaw models validate anthropic
hopclaw doctor auth
hopclaw doctor connectivity
```
