# Runbook: 备份与恢复

## 1. 适用范围

适用于：

- 单实例 HopClaw
- SQLite / JSONL 状态目录
- 审计 outbox 已落本地状态目录

## 2. 备份内容与限制

当前 `hopclaw backup create` 会打包状态目录中的关键文件，并且：

- 跳过 `backups/`
- 跳过 `logs/`
- 跳过 `tmp/`
- 跳过 `.pid` / `.tmp` / `.log`
- 单文件超过 `10MB` 会跳过

因此它适合：

- 运行状态备份
- 配置与审计轨迹归档
- SQLite 单实例恢复

但不等于完整日志保全方案。

## 3. 日常备份建议

### 最低建议

- 每天至少一次 `backup create`
- 保留最近 7 天
- 至少保留一个异地副本

### 变更前强制备份

以下动作前必须手工执行一次备份：

- 升级版本
- 修改生产配置
- 执行恢复演练
- 手工 redrive 大量 dead-letter

## 4. 备份命令

```bash
hopclaw backup create
hopclaw backup list
```

建议同时记录：

```bash
hopclaw config show
hopclaw version
```

## 5. 恢复前检查

恢复前先做这几件事：

1. 停止流量入口，避免恢复过程中继续写入
2. 停止 HopClaw 进程或容器
3. 确认目标机器磁盘可写且容量足够
4. 确认恢复包来源可信

## 6. 恢复步骤

```bash
hopclaw backup restore /path/to/backup-YYYYMMDD-HHMMSS.tar.gz
```

恢复特点：

- 已存在文件会先改名为 `.bak`
- 然后再写入恢复内容

这意味着：

- 恢复具备基本回退痕迹
- 但仍然建议在停机状态执行

## 7. 恢复后验证

启动服务后至少检查：

```bash
curl -fsS http://127.0.0.1:16280/healthz
curl -fsS -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" http://127.0.0.1:16280/operator/controlplane/status
curl -fsS -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" http://127.0.0.1:16280/operator/audit/deliveries/stats
```

再检查：

- 是否能正常进入 dashboard / CLI
- 最近 session 是否可见
- 审计 sink 是否仍注册
- dead-letter 是否异常增长

## 8. 恢复演练建议

每月至少做一次演练：

1. 从最近备份恢复到预发机
2. 启动 HopClaw
3. 检查 `/healthz`
4. 执行一次模型调用
5. 检查审计 sink 列表和 delivery stats

只要没有演练记录，就不要对外宣称“已具备可恢复能力”。
