# Slash Commands

## TL;DR

- In the terminal REPL, slash commands are deterministic and handled locally by the REPL itself.
- `acp/commandsUpdate` is discovery inventory, not an execution contract for the terminal REPL.
- Non-slash input goes through semantic pre-flight. It may become chat, a new run, a run follow-up, a status reply, or another runtime action depending on classifier and policy.
- Outside the REPL, the shared runtime fast-path currently recognizes only `/status`, `/progress`, `/cancel`, and `/abort`.

English is canonical in this file. 中文同步 is at the end.

## Surfaces

HopClaw exposes slash-like control through three different surfaces:

| Surface | Where it applies |
| --- | --- |
| REPL built-ins | `hopclaw` interactive terminal |
| ACP runtime inventory | ACP clients that consume `acp/commandsUpdate` |
| Runtime text commands | Chat or channel surfaces that send plain text into the runtime |

These surfaces are related, but they are not interchangeable. A command that is advertised over ACP is not automatically executable as a REPL slash command.

## Starting The REPL

```bash
hopclaw
hopclaw --session incident-42
hopclaw --remote local-dev
hopclaw --remote https://hopclaw.example.com
hopclaw --local
hopclaw --session incident-42 --model gpt-4.1-mini --think
```

Inside the REPL:

```text
/help
```

## REPL Built-In Commands

These commands are implemented directly by the terminal REPL.

### Task control

| Command | Usage | Meaning |
| --- | --- | --- |
| `/pause` | `/pause` | Pause the current foreground run and keep a resumable handle |
| `/continue` | `/continue [run_id\|session_key]` | Continue the paused task or resume older work |
| `/cancel` | `/cancel` | Cancel the current foreground run without keeping a paused handle |
| `/retry` | `/retry` | Restart the last paused task from the last user turn |
| `/discard` | `/discard` | Drop the current paused handle |
| `/background` | `/background` | Move the current run under background supervision |
| `/bg` | `/bg` | Alias of `/background` |
| `/foreground` | `/foreground <run_id>` | Bring a supervised run back to the foreground |
| `/fg` | `/fg <run_id>` | Alias of `/foreground` |

### Session and context

| Command | Usage | Meaning |
| --- | --- | --- |
| `/session` | `/session [key]` or `/session new [key]` | List, switch, or create sessions |
| `/history` | `/history` | Show recent conversation history |
| `/context` | `/context` | Show context-window usage and memory pressure |
| `/compact` | `/compact` | Compact conversation history |
| `/episode` | `/episode` | Start a new episode in the current session |
| `/reset` | `/reset` | Reset the current session |

### Runtime and connection

| Command | Usage | Meaning |
| --- | --- | --- |
| `/remote` | `/remote [name\|local\|list\|login\|logout]` | Inspect or switch the active connection |
| `/model` | `/model [name]` | Show or change model |
| `/think` | `/think [on\|off]` | Toggle higher-effort reasoning |
| `/cd` | `/cd <path>` | Change local working directory and project context |
| `/view` | `/view [full\|compact\|plain\|auto]` | Change dock layout mode |

### Governance and diagnostics

| Command | Usage | Meaning |
| --- | --- | --- |
| `/approvals` | `/approvals [approve\|deny <id>]` | Inspect or resolve approvals |
| `/quality` | `/quality` | Show quality gate information |
| `/evals` | `/evals [run <suite_id>]` | List or run eval suites |
| `/doctor` | `/doctor` | Show structured readiness and recovery information |
| `/status` | `/status` | Print the current REPL state summary |
| `/runs` | `/runs [recent\|session\|all]` | Inspect recent runs |
| `/last` | `/last` | Show the most recent run summary |

### Identity, automation, and memory

| Command | Usage | Meaning |
| --- | --- | --- |
| `/badge` | `/badge [subcommand]` | Manage the terminal badge |
| `/memory` | `/memory [subcommand]` | Inspect or manage session/project memory |
| `/automation` | `/automation [subcommand]` | Inspect or manage automation from the REPL |
| `/promote` | `/promote` | Promote recent work into automation when available |

### Utility

| Command | Usage | Meaning |
| --- | --- | --- |
| `/help` | `/help [command]` | Show general help or command-specific help |
| `/clear` | `/clear` | Clear the screen and redraw |
| `/exit` | `/exit` | Exit the REPL |
| `/quit` | `/quit` | Alias of `/exit` |

## Canonical Semantics

- `/stop` is not a canonical command in the terminal REPL. Use `/pause`, `/continue`, and `/cancel`.
- `/cancel` means terminate the current run. It does not preserve a resumable paused handle.
- `/pause` means interrupt the current run and keep a resumable paused handle in the REPL.
- `/continue` is the only canonical resume word in the terminal REPL.
- `/remote` is the canonical connection command. User-facing connection terminology is `remote` and `local`.
- Slash commands are deterministic. Unknown slash commands do not fall through to the model.

## Natural Language vs Slash Commands

The terminal input model is:

1. Input starting with `/` is treated as a command and resolved locally by the REPL.
2. If the slash command is unknown, the REPL returns an unknown-command error.
3. Input without `/` goes through semantic pre-flight in the shared runtime.
4. Pre-flight may classify the message as chat, status, task creation, task follow-up, approval reply, or another supported interaction.

That means idle chat is allowed. It is no longer force-promoted into a task run just because it originated from the CLI.

## ACP Runtime Inventory

HopClaw still advertises a runtime command inventory over `acp/commandsUpdate`, but in the terminal REPL it is discovery-only.

Current implications:

- The REPL shows advertised runtime commands in `/help` as advisory inventory.
- `/help <name>` for an advertised runtime command explains that it is discovery-only.
- Typing an advertised runtime command directly in the REPL returns an explicit unsupported-command error.
- The REPL does not treat ACP inventory entries as executable slash commands unless there is a real local built-in.

This is intentional. Today the ACP payload contains command metadata only:

- name
- description
- optional shortcut

It does not define a general execution contract for “run this ACP command”.

### ACP default inventory

The default ACP inventory currently includes:

| Command name | Shortcut | Meaning |
| --- | --- | --- |
| `help` |  | Show available commands |
| `status` |  | Show current session status |
| `context` |  | Show context-window info |
| `usage` |  | Show token usage |
| `cancel` | `/cancel` | Cancel the current run |
| `compact` |  | Compact conversation history |
| `think` | `/think` | Toggle extended thinking |
| `verbose` | `/verbose` | Toggle verbose output |
| `model` | `/model` | Change model |
| `queue` |  | Show run queue |
| `debug` |  | Toggle debug mode |
| `config` |  | Show or set config options |

These entries may be useful to ACP clients with their own execution layer, but the terminal REPL does not promise direct execution for them.

## Runtime Text Commands Outside The REPL

The shared runtime fast-path currently recognizes:

| Input | Meaning |
| --- | --- |
| `/status` | Status query |
| `/progress` | Status query |
| `/cancel` | Cancel the active run |
| `/abort` | Cancel the active run |

`/stop` is no longer part of this canonical set.

## Examples

### Pause, continue, or cancel a foreground run

```text
/pause
/continue
/cancel
```

### Switch connections

```text
/remote
/remote local-dev
/remote local
/remote login prod
```

### Inspect help and inventory

```text
/help
/help model
/help review-pr
```

### Use natural language without forcing a run

```text
hi
what changed in the last run?
```

The shared runtime decides whether that should stay chat-like or become task execution.

## 中文同步

### TL;DR

- 终端 REPL 里的 slash 命令是本地、确定性的。
- `acp/commandsUpdate` 在终端里只是发现能力，不是可执行协议。
- 非 slash 输入会先走共享 runtime 的语义预判，不一定创建 run。
- 非 REPL 文本入口当前只识别 `/status`、`/progress`、`/cancel`、`/abort`。

### 终端里的 canonical 命令

- 任务控制：`/pause`、`/continue`、`/cancel`
- 会话上下文：`/session`、`/history`、`/context`、`/compact`、`/reset`
- 连接与运行时：`/remote`、`/model`、`/think`、`/cd`
- 诊断治理：`/approvals`、`/quality`、`/evals`、`/doctor`
- 其他：`/help`、`/clear`、`/exit`

### 关键语义

- `/stop` 不是当前产品的 canonical 命令。
- `/cancel` 是取消，不保留 paused handle。
- `/pause` 是暂停，保留可继续的 paused handle。
- `/continue` 是终端里唯一的 canonical 继续词。
- 连接术语统一为 `remote` / `local`。

### 动态命令

- ACP 广播的动态命令在终端 REPL 里只作为 inventory 展示。
- `/help <动态命令>` 会明确告诉用户这是 discovery-only。
- 直接输入这类命令会得到显式 unsupported 错误，不会被偷偷发给模型。
