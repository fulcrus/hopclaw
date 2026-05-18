# Runbook: 审计交付排障

## 1. 先确认是什么问题

当前 HopClaw 的审计外发不是 best-effort，而是可靠交付链路：

- 入队
- 重试
- dead-letter
- redrive

所以排障要先区分：

- 是 sink 根本没注册
- 还是 sink 注册了但在重试
- 还是已经进入 dead-letter

## 2. 先看哪些面

```bash
curl -fsS -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  http://127.0.0.1:16280/operator/audit/sinks

curl -fsS -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  http://127.0.0.1:16280/operator/audit/deliveries/stats

curl -fsS -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  "http://127.0.0.1:16280/operator/audit/deliveries?status=dead_letter"
```

Prometheus 指标：

- `hopclaw_audit_delivery_queued_total`
- `hopclaw_audit_delivery_attempts_total{result="retry_scheduled"}`
- `hopclaw_audit_delivery_attempts_total{result="dead_letter"}`

## 3. 常见故障定位

### 3.1 webhook sink

检查：

- URL 是否可达
- 目标服务是否返回 2xx
- HMAC secret 是否一致
- 自定义 header 是否正确

### 3.2 elasticsearch sink

检查：

- `elasticsearch.url` 是否可达
- `elasticsearch.index` 是否存在或允许自动创建
- API Key / Authorization header 是否有效
- 目标集群是否有写入权限

说明：

- HopClaw 对有 `event.id` 的事件使用稳定文档 ID
- 同一事件重试时不会在 Elasticsearch 里生成多份文档

### 3.3 splunk_hec sink

检查：

- HEC URL 是否可达
- Token 是否有效
- Splunk HEC 是否开启
- index/source/sourcetype 是否被 Splunk 侧允许

说明：

- HopClaw 会把 `hopclaw_event_id`、`hopclaw_event_type` 写入 fields
- 如果需要下游幂等聚合，优先用这个键做关联

## 4. dead-letter 处理

先不要直接 redrive 全量死信。

先做：

1. 找出共同错误原因
2. 修复目标系统
3. 按 sink / 时间窗口 / 故障类型分批 redrive

接口：

```bash
curl -X POST \
  -H "Authorization: Bearer $HOPCLAW_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  http://127.0.0.1:16280/operator/audit/deliveries/redrive \
  -d '{"ids":["adel-000001"],"options":{"reset_attempts":true,"clear_error":true}}'
```

## 5. 什么情况该升级为事故

- 5 分钟内出现 dead-letter
- 企业合规链路中断
- 审计记录无法进入检索平台
- retry 数持续飙升且超过阈值

## 6. 推荐告警

- dead-letter 立即 critical
- retry_storm 持续 10 分钟 warning
- `/healthz` 失败 critical

对应规则见：

- `deploy/enterprise/prometheus-rules.yml`
