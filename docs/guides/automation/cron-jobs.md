# Cron Jobs

## TL;DR

- The current CLI surface is `hopclaw automation`, with `cron` as an alias.
- Create cron jobs with `hopclaw automation create`, inspect with `list` / `inspect`, and run them manually with `trigger`.
- Use `hopclaw automation status` and `hopclaw automation recent cron <id>` when debugging missed or failed schedules.

English is canonical in this file. 中文同步 follows after the English section.

## Create A Scheduled Job

Every hour:

```bash
hopclaw automation create \
  --name hourly-health-check \
  --schedule-kind every \
  --every 1h \
  --content "Run a health and readiness summary for this runtime."
```

Cron expression at 09:00 every weekday:

```bash
hopclaw automation create \
  --name weekday-briefing \
  --schedule-kind cron \
  --expression "0 9 * * 1-5" \
  --timezone "Asia/Shanghai" \
  --content "Prepare the morning operator briefing."
```

One-shot run:

```bash
hopclaw automation create \
  --name one-shot \
  --schedule-kind at \
  --at 2026-04-02T09:00:00Z \
  --content "Send the launch checklist."
```

## Inspect And Trigger

```bash
hopclaw automation list --kind cron
hopclaw automation inspect cron <id>
hopclaw automation recent cron <id>
hopclaw automation trigger <id>
hopclaw automation status
```

## Pause And Resume

```bash
hopclaw automation pause cron <id>
hopclaw automation resume cron <id>
```

## Operational Notes

- `--model` lets you override the model used for that scheduled run.
- `--session-key` is useful when you want recurring work to accumulate into one durable thread.
- Use `recent` before editing prompts. Most cron bugs are prompt-quality or delivery-target issues, not scheduler failures.

## 中文同步

### TL;DR

- 当前自动化 CLI 主入口是 `hopclaw automation`，`cron` 只是别名
- 用 `create` 新建定时任务，用 `list` / `inspect` / `recent` 排查
- 需要立刻执行时用 `hopclaw automation trigger <id>`

### 常用命令

```bash
hopclaw automation create --name hourly-health-check --schedule-kind every --every 1h --content "Run a health summary."
hopclaw automation list --kind cron
hopclaw automation inspect cron <id>
hopclaw automation recent cron <id>
hopclaw automation trigger <id>
```
