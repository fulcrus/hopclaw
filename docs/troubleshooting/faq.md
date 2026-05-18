# FAQ

## TL;DR

- Most first-run failures come from config path mistakes, missing provider credentials, or helper processes not actually running.
- Use `hopclaw doctor`, `hopclaw config validate`, and `hopclaw tools list` before you start guessing.
- Keep one clean minimal config that you can always fall back to.

English is canonical in this file. 中文同步 follows after the English section.

## Why does `hopclaw doctor` say provider credentials are missing?

Because HopClaw did not find usable provider credentials in either:

- the current environment
- the active config file

Start with:

```bash
hopclaw doctor auth
hopclaw models list
```

## Why does `hopclaw channels validate <name>` fail even though the config looks right?

Because channel failures are often external:

- revoked token
- wrong app secret
- webhook verification mismatch
- helper or upstream server not reachable

Use:

```bash
hopclaw channels validate <name>
hopclaw doctor connectivity
```

## Why is a browser or desktop tool missing?

Because those tools only appear when the related helper or capability surface is actually available.

Check:

```bash
hopclaw browser status
hopclaw tools search browser
hopclaw tools search nodes
```

## Why does a knowledge source exist but return no search results?

Most often because it has not been synced yet, or the wrong source kind / IDs were used.

Check:

```bash
curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" http://127.0.0.1:16280/operator/knowledge/sources
curl -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" -X POST http://127.0.0.1:16280/operator/knowledge/sources/<id>/sync
```

## Why does a session not contain the messages I expect?

You probably sent work to a different `--session-key` than you thought.

Check:

```bash
hopclaw sessions list
hopclaw sessions get <session-id>
```

## 中文同步

### TL;DR

- 首次使用最常见的问题是配置文件路径错、provider 凭据没生效、helper 实际没跑起来
- 先跑 `hopclaw doctor`、`hopclaw config validate`、`hopclaw tools list`
- 最好保留一份能稳定回退的最小配置

### 高频问题

- provider 凭据缺失：先看 `hopclaw doctor auth`
- channel 校验失败：先看 `hopclaw doctor connectivity`
- browser/desktop 工具没出现：先看 helper 是否真的在线
- knowledge source 没结果：先确认已 sync
