# Local Ollama

## TL;DR

- Ollama uses the built-in `ollama` provider profile with default base URL `http://127.0.0.1:11434/v1`.
- Make sure the local Ollama daemon is serving before you validate.
- `agent.default_model` can stay as the raw local model name, for example `llama3.3`.

English is canonical in this file. 中文同步 follows after the English section.

## Start Ollama First

```bash
ollama serve
ollama pull llama3.3
```

## Minimal YAML

```yaml
agent:
  default_model: "llama3.3"

models:
  default_provider: ollama
  providers:
    ollama:
      api: ollama
      base_url: "http://127.0.0.1:11434/v1"
      default_model: "llama3.3"
```

## Validation Loop

```bash
hopclaw config validate
hopclaw models list
hopclaw models validate ollama
hopclaw doctor connectivity
```

If validation fails, confirm that Ollama is actually reachable:

```bash
curl http://127.0.0.1:11434/api/tags
```

## Notes

- The current provider catalog treats Ollama as a local OpenAI-compatible endpoint.
- No API key is required for the default local setup.
- If you front Ollama with another URL, override `base_url` and keep the model name unqualified.

## 中文同步

### TL;DR

- 本地 Ollama 默认地址是 `http://127.0.0.1:11434/v1`
- 先确认 `ollama serve` 已启动，再跑 `hopclaw models validate ollama`
- `agent.default_model` 可以直接写本地模型名，比如 `llama3.3`

### 最小配置

见上文 YAML；本地默认场景通常不需要 API key。

### 校验命令

```bash
hopclaw config validate
hopclaw models validate ollama
curl http://127.0.0.1:11434/api/tags
```
