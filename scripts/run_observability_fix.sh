#!/bin/bash
# Run Codex to implement observability fixes in one pass.
# Usage: ./scripts/run_observability_fix.sh

set -euo pipefail

PROMPT='你是 HopClaw 项目的高级工程师。请严格按照 scripts/codex_observability_fix.md 的指令一次性完成所有 5 个 Phase 的实现。

关键要求：
1. 先读 scripts/codex_observability_fix.md 整个文件，理解全部 Phase
2. 每个 Phase 的每个文件修改前，先 read 目标文件确认当前代码状态
3. 字段名、方法签名必须与现有代码匹配（ToolCall.Name? Function? 都要确认）
4. 不要修改任何现有 interface 定义
5. 完成后按文档中的 Verification Sequence 依次跑 go build 和 go test

Phase 执行顺序：
Phase 1: go get prometheus + 创建 internal/metrics/metrics.go 和测试
Phase 2: 5 个 instrumentation 点（HTTP middleware → model registry → tool middleware → run lifecycle → approval store → event bus）
Phase 3: tracing correlation（logging/context.go 扩展 + middleware 串联 + run context 传播）
Phase 4: test governance（scripts/test_coverage_gates.sh + Makefile targets）
Phase 5: 所有新代码的单元测试

每完成一个 Phase 立即 go build ./... 确认编译通过再继续下一个。
最终跑 go test ./... 确认全部通过。
如果某个测试失败，诊断修复后继续。不要跳过任何 Phase。

完成后输出: PHASE COMPLETE'

./scripts/codex_autopilot.sh "$PROMPT"
