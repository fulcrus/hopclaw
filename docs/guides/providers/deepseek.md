# DeepSeek Provider

## TL;DR

- DeepSeek is configured as a named provider with OpenAI-compatible completions.
- Set `DEEPSEEK_API_KEY`, then validate with `hopclaw models validate deepseek`.
- Use a qualified runtime model such as `deepseek/deepseek-chat` or `deepseek/deepseek-reasoner`.

English is canonical in this file. 中文同步 follows after the English section.

## Minimal YAML

```yaml
agent:
  default_model: "deepseek/deepseek-chat"

models:
  default_provider: deepseek
  providers:
    deepseek:
      api: openai-completions
      base_url: "https://api.deepseek.com/v1"
      api_key: env:DEEPSEEK_API_KEY
      default_model: "deepseek-chat"
```

## Validation Loop

```bash
hopclaw config validate
hopclaw models list
hopclaw models validate deepseek
hopclaw doctor auth
hopclaw doctor connectivity
```

## Notes

- The provider surface is OpenAI-compatible, but the runtime default model remains provider-qualified in HopClaw config.
- `deepseek-chat` is the safe default when you want a general chat path.
- `deepseek-reasoner` usually deserves an explicit model choice instead of silently replacing your default.

## 中文同步

### TL;DR

- DeepSeek 走命名 provider，API 类型是 `openai-completions`
- 设置 `DEEPSEEK_API_KEY` 后跑 `hopclaw models validate deepseek`
- 运行时默认模型建议写 `deepseek/deepseek-chat`

### 最小配置

见上文 YAML；核心字段是 `base_url`、`api_key`、`default_model`。

### 校验命令

```bash
hopclaw config validate
hopclaw models validate deepseek
hopclaw doctor auth
hopclaw doctor connectivity
```
