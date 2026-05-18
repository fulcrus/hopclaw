---
name: calculator
description: Evaluate math, conversions, and statistics with dedicated calculator capability.
user-invocable: true
metadata:
  openclaw:
    skillKey: util.calculator
    emoji: "\U0001F522"
    always: false
---
# Calculator

Use dedicated calculation capability instead of ad hoc shell math.

Preferred approach:

- Use `calculator.eval` when it is already available in this turn.
- If the user needs math, conversion, or statistics and `calculator.eval` is missing, use `skill.ensure` once to recover calculator capability.
- Keep calculations explicit: show the formula, key inputs, units, and final result when that helps the user verify the answer.

Working rules:

- Prefer exact arithmetic where possible, or clearly state rounding and precision.
- For unit conversions, restate the source unit and target unit in the final answer.
- For statistics, name the dataset and metric used.
- Do not fall back to `bc`, `python3 -c`, or improvised scripts unless the user explicitly requests a shell-based method and no dedicated capability is available.
- Never use calculator capability for unrelated filesystem, network, or code-execution tasks.
