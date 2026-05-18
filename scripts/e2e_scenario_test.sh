#!/usr/bin/env bash
# ---------------------------------------------------------------------------
# e2e_scenario_test.sh — Real end-to-end scenario tests against live HopClaw
#
# Submits user messages via HTTP API, waits for completion, records results.
# Output: .tmp/test-reports/test-report-e2e-*.md
# ---------------------------------------------------------------------------
set -euo pipefail

BASE="http://127.0.0.1:16280"
REPORT_DIR=".tmp/test-reports"
REPORT="$REPORT_DIR/test-report-e2e-$(date +%Y%m%d-%H%M%S).md"
MAX_WAIT=120  # seconds per test case
POLL_INTERVAL=2

mkdir -p "$REPORT_DIR"

# ---------------------------------------------------------------------------
# Helper: submit message, wait for completion, return result JSON
# ---------------------------------------------------------------------------
submit_and_wait() {
    local tc_id="$1"
    local session_key="e2e-${tc_id}"
    local content="$2"

    # Submit
    local submit_resp
    submit_resp=$(curl -s -X POST "${BASE}/runtime/runs" \
        -H "Content-Type: application/json" \
        -d "$(jq -n --arg sk "$session_key" --arg c "$content" \
            '{session_key: $sk, content: $c, execute: true}')" 2>&1)

    local run_id
    run_id=$(echo "$submit_resp" | jq -r '.id // empty')
    if [[ -z "$run_id" ]]; then
        echo "{\"tc\":\"${tc_id}\",\"status\":\"submit_failed\",\"error\":$(echo "$submit_resp" | jq -c '.')}"
        return
    fi

    local status phase session_id
    status=$(echo "$submit_resp" | jq -r '.status // "unknown"')
    session_id=$(echo "$submit_resp" | jq -r '.session_id // empty')

    # If already waiting_input (preflight blocking), that's a valid terminal state for testing
    if [[ "$status" == "waiting_input" ]]; then
        echo "$submit_resp" | jq -c --arg tc "$tc_id" '{tc: $tc, run_id: .id, status: .status, phase: .phase, execution_mode: .execution_mode, job_type: .task_contract.job_type, requires_approval: .task_contract.requires_approval, requires_external_effect: .task_contract.requires_external_effect, missing_info: [.task_contract.missing_info[]?.id], preflight_blocking: .preflight.blocking, preflight_question: .preflight.question, triage_source: .triage.source}'
        return
    fi

    # Poll for completion
    local elapsed=0
    while [[ $elapsed -lt $MAX_WAIT ]]; do
        sleep "$POLL_INTERVAL"
        elapsed=$((elapsed + POLL_INTERVAL))

        local poll_resp
        poll_resp=$(curl -s "${BASE}/runtime/runs/${run_id}" 2>&1)
        status=$(echo "$poll_resp" | jq -r '.status // "unknown"')

        case "$status" in
            completed|failed|cancelled|waiting_input|waiting_approval)
                # Get session for final message
                local session_resp=""
                if [[ -n "$session_id" ]]; then
                    session_resp=$(curl -s "${BASE}/runtime/sessions/${session_id}" 2>&1)
                fi
                local last_assistant_msg
                last_assistant_msg=$(echo "$session_resp" | jq -r '[.messages[]? | select(.role=="assistant")] | last // empty | .content // ""' 2>/dev/null)

                local result_resp
                result_resp=$(curl -s "${BASE}/runtime/runs/${run_id}/result" 2>&1)

                echo "$poll_resp" | jq -c --arg tc "$tc_id" --arg msg "${last_assistant_msg:0:500}" --arg elapsed "$elapsed" \
                    '{tc: $tc, run_id: .id, status: .status, phase: .phase, execution_mode: .execution_mode, job_type: .task_contract.job_type, requires_approval: .task_contract.requires_approval, requires_external_effect: .task_contract.requires_external_effect, missing_info: [.task_contract.missing_info[]?.id], tool_rounds: .tool_rounds, triage_source: .triage.source, elapsed_s: ($elapsed|tonumber), assistant_reply: $msg}'
                return
                ;;
        esac
    done

    echo "{\"tc\":\"${tc_id}\",\"run_id\":\"${run_id}\",\"status\":\"timeout\",\"elapsed_s\":${MAX_WAIT}}"
}

# ---------------------------------------------------------------------------
# Test cases — user's exact words
# ---------------------------------------------------------------------------
declare -A TESTS

# Basic tasks
TESTS[TC-01]="读一下当前仓库，告诉我这是干什么的"
TESTS[TC-03]="把 README 里的安装步骤整理成给同事的 5 条指令"
TESTS[TC-06]="修掉当前 failing test"

# Browser tasks (priority)
TESTS[TC-09]="打开 https://example.com，告诉我标题和首段内容"
TESTS[TC-10]="打开 https://httpbin.org/forms/post，填写表单并提交"
TESTS[TC-13]="页面里有 email 输入框，但这只是填表，不是发消息"
TESTS[TC-14]="页面 URL 里有 /post，但这只是一个网页表单"

# External delivery & approval (priority)
TESTS[TC-23]="直接回复我，不要发到外部渠道"
TESTS[TC-24]="把结果发到飞书给我"
TESTS[TC-25]="取消刚才那个监控提醒"

# Watch mode
TESTS[TC-26]="从现在开始每小时检查 https://example.com，有变化就在当前会话通知我"
TESTS[TC-27]="停掉所有和 example.com 相关的监控"

# Complex chains (priority)
TESTS[TC-32]="读仓库、查启动逻辑、给我 3 个 operational risk，并带文件路径和修复建议"
TESTS[TC-35]="任务执行时间长时，给我一个处理中提示，但不要把中间工具原始 JSON/HTML 当最终答案"
TESTS[TC-36]="如果某个工具失败，换一种方式继续，除非确实无路可走"

# Order for execution
TC_ORDER=(TC-01 TC-03 TC-06 TC-09 TC-10 TC-13 TC-14 TC-23 TC-24 TC-25 TC-26 TC-27 TC-32 TC-35 TC-36)

# ---------------------------------------------------------------------------
# Run tests and build report
# ---------------------------------------------------------------------------
{
    echo "# HopClaw E2E Scenario Test Report"
    echo ""
    echo "**Date:** $(date '+%Y-%m-%d %H:%M:%S')"
    echo "**Server:** ${BASE}"
    echo "**Max wait per TC:** ${MAX_WAIT}s"
    echo ""
    echo "## Results"
    echo ""
    echo "| TC | Status | Mode | Job Type | Approval | ExtEffect | Triage | Time | Notes |"
    echo "|-----|--------|------|----------|----------|-----------|--------|------|-------|"
} > "$REPORT"

PASS=0
FAIL=0
TOTAL=0

for tc_id in "${TC_ORDER[@]}"; do
    content="${TESTS[$tc_id]}"
    TOTAL=$((TOTAL + 1))

    echo ">>> [$tc_id] $content"
    result=$(submit_and_wait "$tc_id" "$content")
    echo "    $result"

    # Parse fields
    status=$(echo "$result" | jq -r '.status // "error"')
    mode=$(echo "$result" | jq -r '.execution_mode // "-"')
    job_type=$(echo "$result" | jq -r '.job_type // "-"')
    approval=$(echo "$result" | jq -r '.requires_approval // false')
    ext_effect=$(echo "$result" | jq -r '.requires_external_effect // false')
    triage_src=$(echo "$result" | jq -r '.triage_source // "-"')
    elapsed=$(echo "$result" | jq -r '.elapsed_s // 0')
    reply=$(echo "$result" | jq -r '.assistant_reply // ""' | head -c 80)
    missing=$(echo "$result" | jq -r '.missing_info // [] | join(",")')

    notes=""
    case "$status" in
        completed) notes="OK" ; PASS=$((PASS + 1)) ;;
        waiting_input) notes="blocked: ${missing}" ;;
        waiting_approval) notes="approval gate" ;;
        failed) notes="FAILED" ; FAIL=$((FAIL + 1)) ;;
        timeout) notes="TIMEOUT" ; FAIL=$((FAIL + 1)) ;;
        *) notes="$status" ;;
    esac

    if [[ -n "$reply" ]]; then
        notes="${notes} | ${reply:0:60}"
    fi

    echo "| $tc_id | $status | $mode | $job_type | $approval | $ext_effect | $triage_src | ${elapsed}s | $notes |" >> "$REPORT"
done

{
    echo ""
    echo "## Summary"
    echo ""
    echo "- **Total:** $TOTAL"
    echo "- **Completed:** $PASS"
    echo "- **Failed/Timeout:** $FAIL"
    echo "- **Blocked (waiting_input):** $((TOTAL - PASS - FAIL))"
} >> "$REPORT"

echo ""
echo "=== Done: $PASS/$TOTAL completed. Report: $REPORT ==="
