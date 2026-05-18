#!/bin/bash

# ============================================================
# HopClaw Round 1 — Phase 提示词生成 & 非交互执行
#
# 用法:
#   ./scripts/run_plan.sh prompt 3     # 生成 Phase 3 的提示词（复制粘贴到 codex 交互模式）
#   ./scripts/run_plan.sh run 3        # 非交互模式执行 Phase 3（codex 退出后自动重启）
#   ./scripts/run_plan.sh run 0 6      # 非交互模式执行 Phase 0 到 6
#   ./scripts/run_plan.sh list         # 列出所有 Phase
# ============================================================

MAX_RETRIES=10
LOG_DIR=".plan_logs"
mkdir -p "$LOG_DIR"

declare -A PHASE_TASKS

PHASE_TASKS[0]="Phase 0：基线冻结与发布门禁

子任务:
1. 运行 go test ./... ，修复所有失败测试，恢复可发布基线
2. 建立 core / supported / experimental 三层门禁标记

结束标准:
- core 全绿
- 关键 smoke tests 全绿
- 无已知 secret 泄露路径
- 无非确定性核心回归"

PHASE_TASKS[1]="Phase 1：安全边界原子切换

子任务:
1. 定义并落地 RuntimeFacts、SecretRef、SecretStatus、ConfigTruth 类型
2. 定义并落地 RunEnvOverlay、SecretResolver 接口及默认实现
3. 实现 BuildRuntimeFacts()，替换所有 runtime facts 构建点（bootstrap runtime context、toolruntime helper、operator skill inspect）
4. 实现 BuildChildEnv()，迁移所有产品 spawn 点（MCP server、stdio channel plugin、skill install、onboard helper、local companion exec）
5. 重写 skill eligibility 数据源：从 Env/Config 切到 SecretPresence / ConfigTruth / Managed / stable facts
6. 重写 env 工具语义：env.get 只返回 redacted diagnostics，env.set 只写 overlay，env.refresh 感知 overlay PATH
7. 配置存储改造：SQLite 配置表不再存明文 secret，仅存非 secret 字段和 SecretRef
8. 写测试验证所有结束标准

结束标准:
- 模型无法读取 secret 明文
- 第三方模块无法读取未声明 secret
- 子进程不再继承整份父进程 env
- existing skills/channels/providers 能力不掉线"

PHASE_TASKS[2]="Phase 2：确定性与排序收敛

子任务:
1. 修复 RuntimeFacts 指纹稳定性，确保同输入产生同指纹
2. 修复 tool pool/domain 裁剪排序、module inventory 排序
3. 修复 channel/provider/operator 列表排序
4. 修复任意 map 迭代进入输出的路径
5. 增加 determinism conformance suite

结束标准:
- 指纹稳定
- tool pool 稳定
- inventory 稳定
- 发布包行为可复现"

PHASE_TASKS[3]="Phase 3：模块统一

子任务:
1. 扩展 ModuleManifest v1，定义三层模块协议（Level 0 Minimal、Level 1 Declared、Level 2 Managed）
2. 所有来源统一转 Module：skill、plugin、MCP、external tool、channel、provider
3. 建立唯一 ModuleRegistry，生成受控 projections（SkillProjection、ToolProjection、ChannelProjection、ProviderProjection）
4. bootstrap/runtime/gateway/toolruntime 全改消费 projection
5. hot-reload 改成 projection version 原子替换
6. 验证所有结束标准

结束标准:
- 无主路径继续直接依赖旧 registry/manager
- 所有能力都能进入统一 module inventory
- 开源作者仍可用 Level 0 低门槛接入"

PHASE_TASKS[4]="Phase 4：渠道能力统一

子任务:
1. 定义 ChannelCapabilityDescriptor
2. 所有 adapter 的 Capabilities() 切到新 descriptor
3. bridge 注入 session.Metadata[\"channel_capabilities\"] 和 interaction metadata
4. 删除基于 session key 前缀的渠道行为猜测
5. agent prompt 改为纯 capability-driven
6. 验证所有结束标准

结束标准:
- adapter 行为路径一致
- interactive/threading/mobile/delivery 行为可预测
- 不再依赖渠道名字符串"

PHASE_TASKS[5]="Phase 5：AuthZ 正式抽象

子任务:
1. 定义 AuthorizationDecider、AuthorizationRequest、AuthorizationDecision 接口和稳定资源/动作枚举
2. 实现 OpenDecider（默认 toC）和 ExternalDecider（企业接入）
3. /operator/authz 替换旧 RBAC 视图
4. 内建 RBAC 迁出核心放到 contrib/authz-rbac
5. 验证所有结束标准

结束标准:
- 核心不再依赖 role matrix
- toC 零配置可用
- toB 可插企业策略"

PHASE_TASKS[6]="Phase 6：国际化与发布治理

子任务:
1. 固定 canonical English contracts（API field、manifest field、error code、policy code、verification code）
2. 中文产品面完整支持，UI/CLI/operator/onboarding 接入统一 i18n catalog
3. 所有测试改成基于 testid / structured state / error code
4. 发布文档：架构文档、模块开发文档、security 模型、channel capability 模型、AuthZ 接入文档

结束标准:
- v1.0 可生产使用
- 默认严格安全
- 模块系统统一
- 渠道行为统一
- AuthZ 抽象正确
- 多语言产品面有稳定基础"

# --- Generate prompt for interactive mode ---
gen_prompt() {
  local phase_num=$1
  local task_text="${PHASE_TASKS[$phase_num]}"

  cat <<EOF
执行 Round 1 的 ${task_text}

参考文档: docs/HopClaw 实施版总方案.md

要求:
- 按子任务顺序逐个实现，全部完成前不要停下来
- 每个子任务完成后 go build ./... 确认编译通过
- 每个子任务完成后运行受影响包的测试
- 全部子任务完成后运行 go test ./... 做最终验证
- 对照结束标准逐条确认
- 发现计划外的问题写到 DISCOVERIES.md，不要当场修
EOF
}

# --- Run in non-interactive mode with auto-restart ---
run_phase() {
  local phase_num=$1
  local task_text="${PHASE_TASKS[$phase_num]}"
  local phase_title
  phase_title=$(echo "$task_text" | head -1)
  local log_file="${LOG_DIR}/phase_${phase_num}.log"
  local tracker=".plan_phase${phase_num}_done.txt"

  > "$tracker"

  echo ""
  echo "========================================="
  echo ">>> 开始: ${phase_title}"
  echo ">>> 模式: 非交互（自动重启）"
  echo "========================================="

  local attempt=0

  while [ $attempt -lt $MAX_RETRIES ]; do
    attempt=$((attempt + 1))

    local done_list=""
    if [ -s "$tracker" ]; then
      done_list=$(cat "$tracker")
    fi

    local prompt
    if [ $attempt -eq 1 ]; then
      prompt="$(gen_prompt "$phase_num")

每完成一个子任务追加一行到 ${tracker}: [DONE] 子任务描述
Phase 全部完成后追加: [PHASE COMPLETE]"
    else
      echo ">>> [重启 #${attempt}] 自动继续..."

      prompt="继续执行 Round 1 的 ${phase_title}。你之前中途退出了。

已完成的子任务（在 ${tracker} 中）:
${done_list:-（无记录，检查代码状态判断）}

完整 Phase 内容:
${task_text}

从下一个未完成的子任务继续，不要重做已完成的。
每完成一个追加到 ${tracker}: [DONE] 子任务描述
全部完成后追加: [PHASE COMPLETE]"
    fi

    codex --approval-mode full-auto "$prompt" 2>&1 | tee -a "$log_file"

    if [ -f "$tracker" ] && grep -q "\[PHASE COMPLETE\]" "$tracker" 2>/dev/null; then
      echo ">>> ${phase_title} 完成 ✓（共 ${attempt} 次调用）"
      return 0
    fi

    if [ -f "$tracker" ] && grep -q "\[BLOCKED\]" "$tracker" 2>/dev/null; then
      echo ">>> 被阻塞:"
      grep "\[BLOCKED\]" "$tracker"
      return 1
    fi

    if [ -f "$tracker" ] && grep -q "\[DECISION NEEDED\]" "$tracker" 2>/dev/null; then
      echo ">>> 需要决策:"
      grep "\[DECISION NEEDED\]" "$tracker"
      return 1
    fi
  done

  echo ">>> 达到最大重试次数 (${MAX_RETRIES})"
  return 1
}

# --- Main ---
case "${1:-}" in
  list)
    echo "可用的 Phase:"
    for i in $(seq 0 6); do
      title=$(echo "${PHASE_TASKS[$i]}" | head -1)
      echo "  $i  $title"
    done
    ;;

  prompt)
    if [ -z "$2" ] || [ -z "${PHASE_TASKS[$2]}" ]; then
      echo "用法: $0 prompt <phase>"
      exit 1
    fi
    echo "--- 复制以下内容粘贴到 codex 交互模式 ---"
    echo ""
    gen_prompt "$2"
    echo ""
    echo "--- 结束 ---"
    ;;

  run)
    if [ -z "$2" ]; then
      echo "用法: $0 run <phase> [to-phase]"
      exit 1
    fi
    from=$2
    to=${3:-$2}
    for phase in $(seq "$from" "$to"); do
      if [ -z "${PHASE_TASKS[$phase]}" ]; then continue; fi
      if ! run_phase "$phase"; then
        echo ">>> Phase $phase 未完成，停止"
        exit 1
      fi
    done
    echo ">>> 全部完成"
    ;;

  *)
    echo "用法:"
    echo "  $0 list              列出所有 Phase"
    echo "  $0 prompt <N>        生成提示词（粘贴到 codex 交互模式）"
    echo "  $0 run <N> [to-N]    非交互模式执行（自动重启）"
    ;;
esac
