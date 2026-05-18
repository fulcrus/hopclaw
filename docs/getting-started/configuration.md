# Configuration

## TL;DR

- Your active config usually lives at `~/.hopclaw/config.yaml`
- Use `hopclaw config path`, `hopclaw config show`, and `hopclaw config validate` before editing by hand
- Keep secrets in environment variables and let YAML reference them

English is canonical in this file. 中文同步 follows after the English section.

## Find The Active Config

```bash
hopclaw config path
hopclaw config show
hopclaw config validate
```

If you want to work with a project-local file instead of the default path:

```bash
hopclaw --config ./local.yaml config validate
hopclaw --config ./local.yaml serve
```

## Minimal Config Skeleton

This shape matches the current release and keeps secrets outside the file:

```yaml
server:
  address: "127.0.0.1:16280"

agent:
  default_model: "gpt-4.1-mini"

models:
  openai_compat:
    base_url: ${OPENAI_BASE_URL}
    api_key: ${OPENAI_API_KEY}
    model: ${OPENAI_MODEL}
    timeout: 60s

runtime:
  profile: desktop

skills:
  install_policy: ask
```

## Safe Editing Workflow

Read a value:

```bash
hopclaw config get server.address
```

Set a value:

```bash
hopclaw config set runtime.profile desktop
```

Remove a value:

```bash
hopclaw config unset skills.install_policy
```

Open the file in your editor:

```bash
hopclaw config edit
```

## Configuration Checks That Matter

After every meaningful edit, run:

```bash
hopclaw config validate
hopclaw doctor config
hopclaw doctor auth
```

## 中文同步

### TL;DR

- 当前生效配置通常在 `~/.hopclaw/config.yaml`
- 手改前先跑 `hopclaw config path`、`hopclaw config show`、`hopclaw config validate`
- Secret 尽量放环境变量，YAML 里只做引用

### 推荐编辑顺序

1. 先用上文 `Find The Active Config` 里的命令确认当前文件
2. 参考上文 `Minimal Config Skeleton` 调整结构
3. 编辑后执行上文 `Configuration Checks That Matter`

