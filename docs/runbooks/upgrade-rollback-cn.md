# Runbook: 升级与回滚

## 1. 升级原则

- 先备份，再升级
- 先预发，再生产
- 先单实例验证，再批量推广
- 不能把配置变更、镜像升级、外部系统切换绑成一次大爆炸

## 2. 升级前清单

升级前确认：

- 当前版本号
- 目标版本号
- 当前生效配置
- 最近一次成功备份
- 模型提供商凭证是否有效
- 企业外部依赖是否可达
  - AuthZ webhook
  - Approval webhook
  - Audit sinks

## 3. 升级步骤

### 第一步：创建备份

```bash
hopclaw backup create
hopclaw backup list
```

### 第二步：记录当前版本与配置

```bash
hopclaw version
hopclaw config show
```

### 第三步：发布新镜像 / 新二进制

- 容器部署：替换镜像标签
- 二进制部署：替换可执行文件

### 第四步：启动并验证

至少验证：

- `/healthz`
- `/operator/controlplane/status`
- `/operator/audit/sinks`
- `/operator/audit/deliveries/stats`
- 一次真实模型调用

## 4. 回滚触发条件

出现以下任一情况，优先回滚：

- 服务无法 ready
- 关键模型调用失败率显著上升
- 审计 dead-letter 持续增加
- Dashboard / CLI 主路径不可用
- 配置解析或启动流程异常

## 5. 回滚步骤

### 快速回滚

1. 切回上一版本镜像 / 二进制
2. 保持原配置不再继续变更
3. 重启并做健康检查

### 数据回滚

如果升级已经造成状态损坏或不一致：

1. 停止服务
2. 使用升级前备份执行 `backup restore`
3. 重新拉起上一版本

## 6. 升级后观察窗口

生产升级后至少观察：

- 15 分钟基础健康
- 1 小时错误率与审计投递
- 24 小时稳定性

重点看：

- `hopclaw_http_request_duration_seconds`
- `hopclaw_model_call_errors_total`
- `hopclaw_audit_delivery_attempts_total`

## 7. 不建议的做法

- 不做备份直接升级
- 一边升级一边临时改配置
- 首次升级就直接跨多个大版本
- 发现 dead-letter 后继续堆积不处理
