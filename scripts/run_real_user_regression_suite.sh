#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BASE="${BASE:-http://127.0.0.1:16280}"
STAMP="$(date +%Y%m%d-%H%M%S)"
REPORT_DIR="$ROOT/.tmp/test-reports"
TMP_DIR="$ROOT/.tmp/regression-$STAMP"
REPORT="$REPORT_DIR/test-report-real-user-regression-$STAMP.md"
RAW="$REPORT_DIR/test-report-real-user-regression-$STAMP.jsonl"
RUN_TIMEOUT_GRACE_S="${RUN_TIMEOUT_GRACE_S:-120}"
RUN_TIMEOUT_GRACE_POLL_S="${RUN_TIMEOUT_GRACE_POLL_S:-5}"
mkdir -p "$REPORT_DIR" "$TMP_DIR" "$ROOT/docs/tmp"

WATCH_SESSION="suite-watch-$STAMP"
DELIVERY_SESSION="suite-delivery-$STAMP"
FILE_SESSION="suite-file-$STAMP"
BROWSER_SESSION="suite-browser-$STAMP"
DESKTOP_SESSION="suite-desktop-$STAMP"
BASE_SESSION="suite-base-$STAMP"
COMPLEX_SESSION="suite-complex-$STAMP"
TC24_TICKET_ID=""
TC24_TICKET_SCOPE=""

trim() {
  awk '{$1=$1; print}'
}

safe_json_or_null() {
  if jq -e . >/dev/null 2>&1 <<<"${1:-}"; then
    printf '%s' "${1:-}"
  else
    printf '%s' 'null'
  fi
}

abs_target_path() {
  local target="$1"
  local dir base
  dir="$(cd "$(dirname "$target")" && pwd)"
  base="$(basename "$target")"
  printf '%s/%s' "$dir" "$base"
}

is_terminal_status() {
  case "${1:-}" in
    completed|failed|cancelled|waiting_input|waiting_approval) return 0 ;;
    *) return 1 ;;
  esac
}

case_entry() {
  case "$1" in
    TC-17|TC-18|TC-19|TC-20|TC-21|TC-22) printf '%s' '本机会话' ;;
    TC-08|TC-23|TC-24|TC-25|TC-26|TC-27|TC-28|TC-29|TC-30|TC-34) printf '%s' '同一 session' ;;
    *) printf '%s' 'Web 控制台' ;;
  esac
}

case_prompt() {
  case "$1" in
    TC-01) printf '%s' '读一下当前仓库，告诉我这是干什么的' ;;
    TC-02) printf '%s' '找出最近 3 个最值得担心的启动风险，并带文件路径' ;;
    TC-03) printf '%s' '把 README 里的安装步骤整理成给同事的 5 条指令' ;;
    TC-04) printf '%s' '比较 README.md 和 README.zh-CN.md 的 browser/desktop 能力说明差异' ;;
    TC-05) printf '%s' '扫描最近的报错日志，帮我判断是配置问题还是代码问题' ;;
    TC-06) printf '%s' '修掉当前 failing test' ;;
    TC-07) printf '%s' '把这段调研结果写到 docs/tmp/report.md' ;;
    TC-08) printf '%s' '继续刚才那个任务，但把结果缩成 3 句话' ;;
    TC-09) printf '%s' '打开 https://example.com，告诉我标题和首段内容' ;;
    TC-10) printf '%s' '打开 https://httpbin.org/forms/post，填写表单并提交' ;;
    TC-11) printf '%s' '打开页面后截图给我，再告诉我页面标题' ;;
    TC-12) printf '%s' '如果按钮点不到，就自己换 selector 再试，不要直接失败' ;;
    TC-13) printf '%s' '页面里有 email 输入框，但这只是填表，不是发消息' ;;
    TC-14) printf '%s' '页面 URL 里有 /post，但这只是一个网页表单' ;;
    TC-15) printf '%s' '打开页面，等搜索结果加载出来，再提取前 5 条' ;;
    TC-16) printf '%s' '抓取页面信息，写到 docs/tmp/example-brief.md' ;;
    TC-17) printf '%s' '打开备忘录/文本编辑器，输入一段文字' ;;
    TC-18) printf '%s' '读一下当前剪贴板，然后帮我整理成 3 条' ;;
    TC-19) printf '%s' '把这段文本写进剪贴板' ;;
    TC-20) printf '%s' '列出当前打开的应用和窗口，告诉我哪个最像浏览器' ;;
    TC-21) printf '%s' '给当前桌面截个图，告诉我屏幕上最显眼的窗口标题' ;;
    TC-22) printf '%s' '切到某个应用窗口，再输入内容' ;;
    TC-23) printf '%s' '直接回复我，不要发到外部渠道' ;;
    TC-24) printf '%s' '把结果发到飞书给我' ;;
    TC-25) printf '%s' '取消刚才那个监控提醒' ;;
    TC-26) printf '%s' '从现在开始每小时检查 https://example.com，有变化就在当前会话通知我' ;;
    TC-27) printf '%s' '停掉所有和 example.com 相关的监控' ;;
    TC-28) printf '%s' '把这份总结发邮件给某人并附带文件' ;;
    TC-29) printf '%s' '需要审批的你就停住等我，不要偷偷执行' ;;
    TC-30) printf '%s' '审批通过后继续刚才那条发送任务' ;;
    TC-31) printf '%s' '打开官网抓信息，整理成 markdown，写入文件，再把路径回复我' ;;
    TC-32) printf '%s' '读仓库、查启动逻辑、给我 3 个 operational risk，并带文件路径和修复建议' ;;
    TC-33) printf '%s' '先读代码，再直接修复发现的一个明确 bug，最后跑相关测试' ;;
    TC-34) printf '%s' '根据页面内容生成一份 brief，然后继续把它改成适合发给老板的版本' ;;
    TC-35) printf '%s' '任务执行时间长时，给我一个处理中提示，但不要把中间工具原始 JSON/HTML 当最终答案' ;;
    TC-36) printf '%s' '如果某个工具失败，换一种方式继续，除非确实无路可走' ;;
    TC-37) printf '%s' '先浏览网页，再结合本地文件内容给我结论' ;;
    TC-38) printf '%s' '只改我指定的文件，不要碰别的地方' ;;
    TC-39) printf '%s' '如果你不确定，就明确说不确定，并告诉我还缺什么证据' ;;
    TC-40) printf '%s' '重复提交同类任务 3 次，检查是否都稳定' ;;
    *) return 1 ;;
  esac
}

case_session() {
  case "$1" in
    TC-01|TC-02|TC-03|TC-04|TC-05) printf '%s' "$BASE_SESSION" ;;
    TC-07|TC-08) printf '%s' "$FILE_SESSION" ;;
    TC-09|TC-10|TC-11|TC-12|TC-13|TC-14|TC-15|TC-16|TC-34|TC-37) printf '%s' "$BROWSER_SESSION" ;;
    TC-17|TC-18|TC-19|TC-20|TC-21|TC-22) printf '%s' "$DESKTOP_SESSION" ;;
    TC-23|TC-24|TC-28|TC-29|TC-30) printf '%s' "$DELIVERY_SESSION" ;;
    TC-25|TC-26|TC-27) printf '%s' "$WATCH_SESSION" ;;
    *) printf '%s' "$COMPLEX_SESSION-$1" ;;
  esac
}

case_timeout() {
  case "$1" in
    TC-09|TC-10|TC-11|TC-12|TC-13|TC-14|TC-15|TC-16|TC-34|TC-37) printf '%s' '75' ;;
    TC-06|TC-31|TC-32|TC-33|TC-35|TC-36|TC-40) printf '%s' '90' ;;
    *) printf '%s' '45' ;;
  esac
}

browser_prime_prompt() {
  case "$1" in
    TC-13|TC-14) printf '%s' '打开 https://httpbin.org/forms/post，确认表单页面已加载，然后保持在当前页面，不要提交。' ;;
    TC-15) printf '%s' '打开 https://www.bing.com/search?q=openai，等搜索结果显示出来后保持在当前页面，不要提取内容。' ;;
    TC-16) printf '%s' '打开 https://example.com 并保持在当前页面，不要继续分析。' ;;
    *) return 1 ;;
  esac
}

case_expected_path() {
  case "$1" in
    TC-07) printf '%s' "$ROOT/docs/tmp/report.md" ;;
    TC-16) printf '%s' "$ROOT/docs/tmp/example-brief.md" ;;
    *) printf '%s' '' ;;
  esac
}

get_git_status() {
  git -C "$ROOT" status --short
}

resolve_tc24_approval_if_needed() {
  if [ -z "$TC24_TICKET_ID" ]; then
    return 0
  fi
  local body
  if [ -n "$TC24_TICKET_SCOPE" ]; then
    body="$(jq -n --arg status approved --arg scope "$TC24_TICKET_SCOPE" --arg by regression-suite '{status:$status, scope:$scope, by:$by, note:"approved by regression suite"}')"
  else
    body="$(jq -n --arg status approved --arg by regression-suite '{status:$status, by:$by, note:"approved by regression suite"}')"
  fi
  curl -s -X POST "$BASE/operator/approvals/$TC24_TICKET_ID/resolve" \
    -H 'Content-Type: application/json' \
    -d "$body" >/dev/null || true
}

run_once() {
  local case_id="$1"
  local session_key="$2"
  local prompt="$3"
  local timeout_s="$4"
  local mid_completion_file="$5"

  local submit_body submit_resp run_id submit_status
  submit_body="$(jq -n --arg sk "$session_key" --arg c "$prompt" '{session_key:$sk, content:$c, execute:true}')"
  submit_resp="$(curl -s -X POST "$BASE/runtime/runs" -H 'Content-Type: application/json' -d "$submit_body")"
  run_id="$(jq -r '.id // empty' <<<"$submit_resp")"
  if [ -z "$run_id" ]; then
    jq -n \
      --arg case_id "$case_id" \
      --arg prompt "$prompt" \
      --arg session_key "$session_key" \
      --arg error "$submit_resp" \
      '{test_id:$case_id, session_key:$session_key, user_prompt:$prompt, submit_error:$error}' >"$TMP_DIR/$case_id.submit.json"
    return 1
  fi

  printf '%s\n' "$submit_resp" >"$TMP_DIR/$case_id.submit.json"
  local run_json="" result_json="" completion_json="" approvals_json=""
  local status="queued" elapsed=0
  local mid_captured=0
  while [ "$elapsed" -lt "$timeout_s" ]; do
    run_json="$(curl -s "$BASE/runtime/runs/$run_id")"
    status="$(jq -r '.status // "unknown"' <<<"$run_json")"
    if is_terminal_status "$status"; then
      break
    fi
    sleep 2
    elapsed=$((elapsed + 2))
    if [ "$case_id" = "TC-35" ] && [ "$mid_captured" -eq 0 ] && [ "$elapsed" -ge 4 ]; then
      curl -s "$BASE/runtime/runs/$run_id/completion" >"$mid_completion_file" || true
      mid_captured=1
    fi
  done
  if ! is_terminal_status "$status" && [ "${RUN_TIMEOUT_GRACE_S:-0}" -gt 0 ]; then
    local grace_elapsed=0
    while [ "$grace_elapsed" -lt "$RUN_TIMEOUT_GRACE_S" ]; do
      sleep "$RUN_TIMEOUT_GRACE_POLL_S"
      grace_elapsed=$((grace_elapsed + RUN_TIMEOUT_GRACE_POLL_S))
      run_json="$(curl -s "$BASE/runtime/runs/$run_id")"
      status="$(jq -r '.status // "unknown"' <<<"$run_json")"
      if is_terminal_status "$status"; then
        break
      fi
    done
  fi
  if [ -z "$run_json" ]; then
    run_json="$(curl -s "$BASE/runtime/runs/$run_id")"
    status="$(jq -r '.status // "unknown"' <<<"$run_json")"
  fi
  result_json="$(curl -s "$BASE/runtime/runs/$run_id/result" || true)"
  completion_json="$(curl -s "$BASE/runtime/runs/$run_id/completion" || true)"
  approvals_json="$(curl -s "$BASE/operator/approvals?status=pending&run_id=$run_id&limit=20" || true)"

  printf '%s\n' "$run_json" >"$TMP_DIR/$case_id.run.json"
  printf '%s\n' "$result_json" >"$TMP_DIR/$case_id.result.json"
  printf '%s\n' "$completion_json" >"$TMP_DIR/$case_id.completion.json"
  printf '%s\n' "$approvals_json" >"$TMP_DIR/$case_id.approvals.json"

  result_json="$(safe_json_or_null "$result_json")"
  completion_json="$(safe_json_or_null "$completion_json")"
  approvals_json="$(safe_json_or_null "$approvals_json")"
  run_json="$(safe_json_or_null "$run_json")"

  jq -n \
    --arg case_id "$case_id" \
    --arg session_key "$session_key" \
    --arg prompt "$prompt" \
    --arg run_id "$run_id" \
    --argjson submit "$(cat "$TMP_DIR/$case_id.submit.json")" \
    --argjson run "$run_json" \
    --argjson result "$result_json" \
    --argjson completion "$completion_json" \
    --argjson approvals "$approvals_json" \
    '{test_id:$case_id, session_key:$session_key, user_prompt:$prompt, run_id:$run_id, submit:$submit, run:$run, result:$result, completion:$completion, approvals:$approvals}' \
    >"$TMP_DIR/$case_id.bundle.json"
}

prime_browser_context() {
  local case_id="$1"
  local session_key="$2"
  local prompt prime_prompt

  if ! prime_prompt="$(browser_prime_prompt "$case_id" 2>/dev/null)"; then
    return 0
  fi

  prompt="$(trim <<<"$prime_prompt")"
  if [ -z "$prompt" ]; then
    return 0
  fi

  : >"$TMP_DIR/$case_id.prime.mid_completion.json"
  run_once "PRIME-$case_id" "$session_key" "$prompt" 45 "$TMP_DIR/$case_id.prime.mid_completion.json" || true
}

write_case_record() {
  local case_id="$1"
  local entry="$2"
  local prompt="$3"
  local expected_path="$4"
  local before_git_file="$5"
  local after_git_file="$6"
  local mid_completion_file="$7"
  local bundle_file="$TMP_DIR/$case_id.bundle.json"

  local run_id status phase exec_mode tool_rounds job_type final_response verification_status verification_summary
  local approval_required issue_description evidence_or_artifact misclassification dead_loop false_success approval_correct
  local file_note git_note mid_note ticket_id ticket_scope preflight_question deliverables_text

  run_id="$(jq -r '.run_id // empty' "$bundle_file")"
  status="$(jq -r '.run.status // .result.status // "unknown"' "$bundle_file")"
  phase="$(jq -r '.run.phase // empty' "$bundle_file")"
  exec_mode="$(jq -r '.run.execution_mode // empty' "$bundle_file")"
  tool_rounds="$(jq -r '.run.tool_rounds // 0' "$bundle_file")"
  job_type="$(jq -r '.run.task_contract.job_type // .result.task_contract.job_type // empty' "$bundle_file")"
  final_response="$(jq -r '.result.output // .result.summary // .completion.result.output // .run.preflight.question // empty' "$bundle_file")"
  verification_status="$(jq -r '.result.verification_status // .completion.verification.status // empty' "$bundle_file")"
  verification_summary="$(jq -r '.result.verification_summary // .completion.verification.summary // empty' "$bundle_file")"
  preflight_question="$(jq -r '.run.preflight.question // empty' "$bundle_file")"
  ticket_id="$(jq -r '.approvals.items[0].id // empty' "$bundle_file")"
  ticket_scope="$(jq -r '.approvals.items[0].scope // empty' "$bundle_file")"
  deliverables_text="$(jq -r '[.result.bundle.deliverables[]?.uri // .completion.bundle.deliverables[]?.uri] | unique | join(", ")' "$bundle_file")"

  if [ -n "$ticket_id" ]; then
    approval_required="yes"
  elif [ "$status" = "waiting_approval" ]; then
    approval_required="yes"
  elif [ "$(jq -r '.run.task_contract.requires_approval // false' "$bundle_file")" = "true" ]; then
    approval_required="yes"
  else
    approval_required="no"
  fi

  if [ -n "$expected_path" ] && [ -f "$expected_path" ]; then
    file_note="file=$(abs_target_path "$expected_path")"
  elif [ -n "$expected_path" ]; then
    file_note="missing_expected_file=$(abs_target_path "$expected_path")"
  else
    file_note=""
  fi

  git_note=""
  if ! diff -u "$before_git_file" "$after_git_file" >/dev/null 2>&1; then
    git_note="git_changed=$(diff -u "$before_git_file" "$after_git_file" | tail -n +3 | sed 's/^/    /' | head -n 40)"
  fi

  mid_note=""
  if [ -s "$mid_completion_file" ]; then
    mid_note="$(jq -r 'if type=="object" then (.summary // .result.summary // .verification.summary // "") else "" end' "$mid_completion_file" 2>/dev/null || true)"
    if [ -z "$mid_note" ]; then
      mid_note="$(head -c 120 "$mid_completion_file" | tr '\n' ' ')"
    fi
  fi

  evidence_or_artifact="$deliverables_text"
  if [ -n "$file_note" ]; then
    evidence_or_artifact="${evidence_or_artifact}${evidence_or_artifact:+; }$file_note"
  fi
  if [ -n "$ticket_id" ]; then
    evidence_or_artifact="${evidence_or_artifact}${evidence_or_artifact:+; }approval_ticket=$ticket_id"
  fi
  if [ -z "$evidence_or_artifact" ] && [ -n "$verification_summary" ]; then
    evidence_or_artifact="verification=$verification_summary"
  fi
  if [ -z "$evidence_or_artifact" ]; then
    evidence_or_artifact="none"
  fi

  misclassification="no"
  case "$case_id" in
    TC-13|TC-14|TC-23)
      if [ "$job_type" = "message_delivery" ] || [ "$job_type" = "email_delivery" ] || [ "$(jq -r '.run.task_contract.requires_external_effect // false' "$bundle_file")" = "true" ]; then
        misclassification="yes"
      fi
      ;;
    TC-25)
      if [ -n "$preflight_question" ] && grep -qi 'monitor' <<<"$preflight_question"; then
        misclassification="yes"
      fi
      ;;
  esac

  dead_loop="no"
  if [ "$status" = "running" ] || [ "$status" = "queued" ]; then
    dead_loop="yes"
  elif [ "$tool_rounds" -ge 12 ] && [ "$status" != "completed" ]; then
    dead_loop="yes"
  fi

  false_success="no"
  if [ "$status" = "completed" ] && [ -n "$expected_path" ] && [ ! -f "$expected_path" ]; then
    false_success="yes"
  elif [ "$status" = "completed" ] && [ "$verification_status" = "failed" ]; then
    false_success="yes"
  fi

  approval_correct="n/a"
  case "$case_id" in
    TC-23|TC-25)
      if [ "$approval_required" = "no" ]; then approval_correct="yes"; else approval_correct="no"; fi
      ;;
    TC-24|TC-28|TC-29|TC-30)
      if [ "$approval_required" = "yes" ] || [ "$status" = "completed" ] || [ "$status" = "waiting_input" ]; then approval_correct="yes"; else approval_correct="no"; fi
      ;;
  esac

  case "$status" in
    waiting_input)
      issue_description="needs input: ${preflight_question:-missing required context}"
      ;;
    waiting_approval)
      issue_description="waiting approval"
      ;;
    failed)
      issue_description="$(jq -r '.result.summary // .completion.result.summary // .completion.verification.summary // "run failed"' "$bundle_file")"
      ;;
    completed)
      if [ -n "$verification_summary" ] && [ "$verification_status" != "passed" ]; then
        issue_description="$verification_summary"
      elif [ -n "$mid_note" ] && [ "$case_id" = "TC-35" ]; then
        issue_description="mid_summary=${mid_note}"
      else
        issue_description=""
      fi
      ;;
    *)
      issue_description="did not reach a stable terminal state"
      ;;
  esac
  if [ -n "$git_note" ]; then
    issue_description="${issue_description}${issue_description:+; }${git_note}"
  fi

  if [ "$case_id" = "TC-24" ] && [ -n "$ticket_id" ]; then
    TC24_TICKET_ID="$ticket_id"
    TC24_TICKET_SCOPE="$ticket_scope"
  fi

  jq -n \
    --arg test_id "$case_id" \
    --arg user_entry "$entry" \
    --arg user_prompt "$prompt" \
    --arg run_id "$run_id" \
    --arg final_status "$status${verification_status:+ / $verification_status}" \
    --arg final_response "$final_response" \
    --arg verification "${verification_status:-unknown}${verification_summary:+ | $verification_summary}" \
    --arg approval_required "$approval_required" \
    --arg approval_handled_correctly "$approval_correct" \
    --arg evidence_or_artifact "$evidence_or_artifact" \
    --arg misclassification "$misclassification" \
    --arg dead_loop "$dead_loop" \
    --arg false_success "$false_success" \
    --arg issue_description "$issue_description" \
    '{test_id:$test_id,user_entry:$user_entry,user_prompt:$user_prompt,run_id:$run_id,final_status:$final_status,final_response:$final_response,verification:$verification,approval_required:$approval_required,approval_handled_correctly:$approval_handled_correctly,evidence_or_artifact:$evidence_or_artifact,misclassification:$misclassification,dead_loop:$dead_loop,false_success:$false_success,issue_description:$issue_description}' \
    | tee -a "$RAW" >/dev/null
}

run_case() {
  local case_id="$1"
  local prompt session_key entry timeout_s expected_path
  local before_git_file="$TMP_DIR/$case_id.before.git"
  local after_git_file="$TMP_DIR/$case_id.after.git"
  local mid_completion_file="$TMP_DIR/$case_id.mid_completion.json"

  prompt="$(case_prompt "$case_id")"
  session_key="$(case_session "$case_id")"
  entry="$(case_entry "$case_id")"
  timeout_s="$(case_timeout "$case_id")"
  expected_path="$(case_expected_path "$case_id")"

  get_git_status >"$before_git_file"
  : >"$mid_completion_file"

  if [ "$session_key" = "$BROWSER_SESSION" ]; then
    prime_browser_context "$case_id" "$session_key"
  fi

  if [ "$case_id" = "TC-30" ]; then
    resolve_tc24_approval_if_needed
    sleep 2
  fi

  if run_once "$case_id" "$session_key" "$prompt" "$timeout_s" "$mid_completion_file"; then
    :
  else
    jq -n \
      --arg test_id "$case_id" \
      --arg user_entry "$entry" \
      --arg user_prompt "$prompt" \
      --arg final_status "submit_failed" \
      --arg issue_description "failed to create run" \
      '{test_id:$test_id,user_entry:$user_entry,user_prompt:$user_prompt,final_status:$final_status,issue_description:$issue_description}' \
      | tee -a "$RAW" >/dev/null
  fi
  get_git_status >"$after_git_file"
  if [ -f "$TMP_DIR/$case_id.bundle.json" ]; then
    write_case_record "$case_id" "$entry" "$prompt" "$expected_path" "$before_git_file" "$after_git_file" "$mid_completion_file"
  fi
}

run_tc40() {
  local prompt='打开 https://example.com，告诉我标题和首段内容'
  local session_prefix="suite-stability-$STAMP"
  local statuses=()
  local run_ids=()
  local idx
  for idx in 1 2 3; do
    local case_id="TC-40-$idx"
    run_once "$case_id" "$session_prefix-$idx" "$prompt" 45 "$TMP_DIR/$case_id.mid.json" || true
    if [ -f "$TMP_DIR/$case_id.bundle.json" ]; then
      statuses+=("$(jq -r '.run.status // "unknown"' "$TMP_DIR/$case_id.bundle.json")/$(jq -r '.result.verification_status // .completion.verification.status // "unknown"' "$TMP_DIR/$case_id.bundle.json")")
      run_ids+=("$(jq -r '.run_id // empty' "$TMP_DIR/$case_id.bundle.json")")
    fi
  done
  local consistency="stable"
  if [ "${#statuses[@]}" -ne 3 ] || [ "${statuses[0]:-}" != "${statuses[1]:-}" ] || [ "${statuses[1]:-}" != "${statuses[2]:-}" ]; then
    consistency="unstable"
  fi
  jq -n \
    --arg test_id "TC-40" \
    --arg user_entry "$(case_entry TC-40)" \
    --arg user_prompt "$(case_prompt TC-40)" \
    --arg run_id "$(IFS=,; printf '%s' "${run_ids[*]}")" \
    --arg final_status "$consistency" \
    --arg verification "$(IFS=,; printf '%s' "${statuses[*]}")" \
    --arg approval_required "no" \
    --arg approval_handled_correctly "n/a" \
    --arg evidence_or_artifact "repeated_prompt=$prompt" \
    --arg misclassification "n/a" \
    --arg dead_loop "no" \
    --arg false_success "$([ "$consistency" = "stable" ] && printf no || printf yes)" \
    --arg issue_description "$([ "$consistency" = "stable" ] && printf '' || printf 'same task produced inconsistent terminal results')" \
    '{test_id:$test_id,user_entry:$user_entry,user_prompt:$user_prompt,run_id:$run_id,final_status:$final_status,verification:$verification,approval_required:$approval_required,approval_handled_correctly:$approval_handled_correctly,evidence_or_artifact:$evidence_or_artifact,misclassification:$misclassification,dead_loop:$dead_loop,false_success:$false_success,issue_description:$issue_description}' \
    | tee -a "$RAW" >/dev/null
}

write_report() {
  {
    echo "# HopClaw Real-User Regression Report"
    echo
    echo "- date: $(date '+%F %T %Z')"
    echo "- base: $BASE"
    echo "- raw: $(realpath "$RAW")"
    echo
    echo "## Summary"
    echo
    echo "| TC | Final Status | Approval | Misclass | Loop | False Success | Run ID |"
    echo "| --- | --- | --- | --- | --- | --- | --- |"
    jq -r '[.test_id, .final_status, .approval_required, .misclassification, .dead_loop, .false_success, .run_id] | @tsv' "$RAW" | while IFS=$'\t' read -r tc final_status approval_required misclassification dead_loop false_success run_id; do
      printf '| %s | %s | %s | %s | %s | %s | %s |\n' "$tc" "$final_status" "$approval_required" "$misclassification" "$dead_loop" "$false_success" "$run_id"
    done
    echo
    echo "## Details"
    echo
    jq -c '.' "$RAW" | while IFS= read -r row; do
      echo "### $(jq -r '.test_id' <<<"$row")"
      echo
      echo "- test_id: $(jq -r '.test_id' <<<"$row")"
      echo "- user_entry: $(jq -r '.user_entry' <<<"$row")"
      echo "- user_prompt: $(jq -r '.user_prompt' <<<"$row")"
      echo "- run_id: $(jq -r '.run_id // ""' <<<"$row")"
      echo "- final_status: $(jq -r '.final_status // ""' <<<"$row")"
      echo "- final_response: $(jq -r '.final_response // ""' <<<"$row" | tr '\n' ' ' | sed 's/  */ /g')"
      echo "- verification: $(jq -r '.verification // ""' <<<"$row")"
      echo "- approval_required: $(jq -r '.approval_required // ""' <<<"$row")"
      echo "- approval_handled_correctly: $(jq -r '.approval_handled_correctly // ""' <<<"$row")"
      echo "- evidence_or_artifact: $(jq -r '.evidence_or_artifact // ""' <<<"$row" | tr '\n' ' ' | sed 's/  */ /g')"
      echo "- misclassification: $(jq -r '.misclassification // ""' <<<"$row")"
      echo "- dead_loop: $(jq -r '.dead_loop // ""' <<<"$row")"
      echo "- false_success: $(jq -r '.false_success // ""' <<<"$row")"
      echo "- issue_description: $(jq -r '.issue_description // ""' <<<"$row" | tr '\n' ' ' | sed 's/  */ /g')"
      echo
    done
  } >"$REPORT"
}

main() {
  : >"$RAW"
  local order=()
  if [ "$#" -gt 0 ]; then
    order=("$@")
  else
    order=(
      TC-01 TC-02 TC-03 TC-04 TC-05
      TC-07 TC-08
      TC-09 TC-10 TC-11 TC-12 TC-13 TC-14 TC-15 TC-16
      TC-17 TC-18 TC-19 TC-20 TC-21 TC-22
      TC-23 TC-24 TC-29 TC-30
      TC-26 TC-25 TC-27
      TC-28
      TC-31 TC-32 TC-34 TC-35 TC-36 TC-37 TC-39
      TC-06 TC-33 TC-38
    )
  fi
  local case_id
  for case_id in "${order[@]}"; do
    echo "==> $case_id $(case_prompt "$case_id")"
    if ! run_case "$case_id"; then
      jq -n \
        --arg test_id "$case_id" \
        --arg user_entry "$(case_entry "$case_id")" \
        --arg user_prompt "$(case_prompt "$case_id")" \
        --arg final_status "runner_error" \
        --arg issue_description "regression harness failed while collecting this case" \
        '{test_id:$test_id,user_entry:$user_entry,user_prompt:$user_prompt,final_status:$final_status,issue_description:$issue_description}' \
        | tee -a "$RAW" >/dev/null
    fi
    sleep 1
  done
  if [ "$#" -eq 0 ] || printf '%s\n' "$@" | grep -qx 'TC-40'; then
    run_tc40 || true
  fi
  write_report
  echo "$REPORT"
}

main "$@"
