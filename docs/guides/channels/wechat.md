# WeChat Status

## TL;DR

- This repository snapshot does not currently ship a first-party built-in `wechat` channel adapter.
- If you need WeChat today, use an external bridge plus `webhook`, a plugin surface, or another already-supported channel for the control path.
- Do not search for `channels.wechat` in config and assume you missed a hidden feature. It is not part of the shipped built-in catalog right now.

English is canonical in this file. 中文同步 follows after the English section.

## Current State

The Task 1 documentation tree originally reserved a `wechat.md` slot, but the current product catalog and `channels/` runtime surface do not expose a built-in WeChat adapter.

That means:

- no `channels.wechat` config block
- no `hopclaw channels validate wechat` built-in flow
- no setup/onboard path for a first-party WeChat adapter in this repo snapshot

## Practical Alternatives

Use one of these depending on your integration style:

- `channels.webhook` when an external bridge can call or receive HTTP
- plugin or host-backed integration when you need a typed private bridge
- another built-in channel for operator control, while WeChat stays downstream from your own bridge

## 中文同步

### TL;DR

- 当前仓库快照里没有内建的一方 `wechat` channel adapter
- 如果今天必须接 WeChat，请走外部 bridge + `webhook`、插件面，或其他已支持渠道
- 不要继续找 `channels.wechat` 这个配置块了，它现在并不存在
