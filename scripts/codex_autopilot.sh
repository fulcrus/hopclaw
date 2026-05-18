#!/bin/bash

# ============================================================
# Codex 自动驾驶（非交互模式）
#
# 用法:
#   ./scripts/codex_autopilot.sh "执行 Phase 3..."
#   ./scripts/codex_autopilot.sh                      # 用默认续推指令
#   IDLE_SECONDS=90 MAX_RETRIES=20 ./scripts/codex_autopilot.sh "..."
#
# 原理: 用 codex exec（非交互），跑完自动重启继续，直到 Phase 完成。
# 输出干净无乱码，可直接看日志。
# ============================================================

MAX_RETRIES=${MAX_RETRIES:-15}
LOG_DIR=".plan_logs"
mkdir -p "$LOG_DIR"

CODEX_BIN=$(which codex 2>/dev/null)
if [ -z "$CODEX_BIN" ]; then
  echo "找不到 codex 命令"
  exit 1
fi

INITIAL_PROMPT="${1:-}"
CONTINUE_MSG="${CONTINUE_MSG:-按照 docs/HopClaw 实施版总方案.md ，继续推进。检查当前代码状态判断上次做到哪了，从下一个未完成的子任务继续。不要重做已完成的工作。}"
TRACKER=".plan_tracker.md"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
LOG_FILE="${LOG_DIR}/autopilot_${TIMESTAMP}.log"

log() {
  echo "[$(date '+%H:%M:%S')] $*" | tee -a "$LOG_FILE"
}

# --- 初始化 tracker ---
if [ ! -f "$TRACKER" ]; then
  echo "# Plan Execution Tracker" > "$TRACKER"
  echo "" >> "$TRACKER"
fi

log "========================================="
log "Codex 自动驾驶启动"
log "日志: $LOG_FILE"
log "追踪: $TRACKER"
log "最多重启: $MAX_RETRIES 次"
log "========================================="

attempt=0
last_message_file=$(mktemp)

while [ $attempt -lt $MAX_RETRIES ]; do
  attempt=$((attempt + 1))

  # 构建 prompt
  if [ $attempt -eq 1 ] && [ -n "$INITIAL_PROMPT" ]; then
    prompt="$INITIAL_PROMPT"
  else
    if [ $attempt -gt 1 ]; then
      log ""
      log ">>> [重启 #${attempt}] codex 退出，自动继续..."
    fi
    prompt="$CONTINUE_MSG"
  fi

  log ""
  log "========================================="
  log ">>> 第 ${attempt} 轮 开始"
  log ">>> Prompt: ${prompt:0:100}..."
  log "========================================="
  log ""

  # 运行 codex exec，输出同时到终端和日志
  "$CODEX_BIN" exec --full-auto \
    -o "$last_message_file" \
    "$prompt" 2>&1 | tee -a "$LOG_FILE"

  exit_code=$?
  log ""
  log ">>> 第 ${attempt} 轮 结束 (exit code: $exit_code)"

  # 记录最后输出
  if [ -f "$last_message_file" ] && [ -s "$last_message_file" ]; then
    last_msg=$(cat "$last_message_file")
    log ">>> 最后消息: ${last_msg:0:200}"

    # 追加到 tracker
    echo "" >> "$TRACKER"
    echo "## Round $attempt ($(date '+%Y-%m-%d %H:%M'))" >> "$TRACKER"
    echo "$last_msg" >> "$TRACKER"
  fi

  # 检查是否 Phase 完成
  if [ -f "$last_message_file" ] && grep -qi "phase.*complete\|phase.*完成\|PHASE COMPLETE\|结束标准.*全部" "$last_message_file" 2>/dev/null; then
    log ""
    log "========================================="
    log ">>> Phase 完成 ✓（共 ${attempt} 轮）"
    log "========================================="
    rm -f "$last_message_file"
    exit 0
  fi

  # 检查是否被阻塞
  if [ -f "$last_message_file" ] && grep -qi "BLOCKED\|DECISION NEEDED" "$last_message_file" 2>/dev/null; then
    log ""
    log ">>> 需要人工介入，停止自动驾驶"
    rm -f "$last_message_file"
    exit 1
  fi

  # 检查是否 429 限流
  if [ -f "$last_message_file" ] && grep -q "429" "$last_message_file" 2>/dev/null; then
    log ">>> API 限流 (429)，等待 120 秒后重试..."
    sleep 120
  elif grep -q "429 Too Many Requests" "$LOG_FILE" 2>/dev/null && [ "$(tail -5 "$LOG_FILE" | grep -c "429")" -gt 0 ]; then
    log ">>> API 限流 (429)，等待 120 秒后重试..."
    sleep 120
  else
    log ">>> Phase 未完成，准备下一轮..."
    sleep 3
  fi
done

log ""
log "========================================="
log ">>> 达到最大轮次 (${MAX_RETRIES})，停止"
log ">>> 查看进度: cat $TRACKER"
log "========================================="
rm -f "$last_message_file"
exit 1
