package repl

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestTerminalPrompterReadRichLineFallsBackToReadLineOnNonTTY(t *testing.T) {
	in, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	defer in.Close()

	if _, err := io.WriteString(writer, "hello rich world\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	var out bytes.Buffer
	prompter := NewTerminalPrompter(in, &out, NewHistory("", 10))
	if prompter.tty {
		t.Fatal("expected pipe-backed prompter to be non-TTY")
	}

	result, err := prompter.ReadRichLine("> ", NewCommandRegistry())
	if err != nil {
		t.Fatalf("ReadRichLine() error = %v", err)
	}
	if result.Text != "hello rich world" {
		t.Fatalf("result.Text = %q, want %q", result.Text, "hello rich world")
	}
	if len(result.Images) != 0 {
		t.Fatalf("result.Images = %#v, want empty", result.Images)
	}
	if out.String() != "> " {
		t.Fatalf("prompt output = %q, want %q", out.String(), "> ")
	}
}

func TestDynamicPromptUsesWorkbenchPrompts(t *testing.T) {
	prompt := &DynamicPrompt{
		stateProvider: func() REPLViewState {
			return REPLViewState{
				Target:     "local",
				SessionKey: "default",
				CWD:        "/tmp/hopclaw",
			}
		},
	}
	if got := prompt.Input(); got != "> " {
		t.Fatalf("prompt.Input() = %q, want %q", got, "> ")
	}
	prompt.SetApproval(true)
	if got := prompt.Input(); got != "approval> " {
		t.Fatalf("prompt.Input() with approval = %q, want %q", got, "approval> ")
	}
	prompt.SetApproval(false)
	prompt.SetPaused(true)
	if got := prompt.Input(); got != "paused> " {
		t.Fatalf("prompt.Input() with paused = %q, want %q", got, "paused> ")
	}
	if got := prompt.Approval(); got != "approval> " {
		t.Fatalf("prompt.Approval() = %q, want %q", got, "approval> ")
	}

	prompt = &DynamicPrompt{}
	prompt.SetTarget("prod")
	if got := prompt.Input(); got != "> " {
		t.Fatalf("prompt.Input() with fallback target = %q, want %q", got, "> ")
	}

	prompt = &DynamicPrompt{
		stateProvider: func() REPLViewState {
			return REPLViewState{
				ExecutionState: "waiting approval",
			}
		},
	}
	if got := prompt.Input(); got != "approval> " {
		t.Fatalf("prompt.Input() from state provider = %q, want %q", got, "approval> ")
	}
}

func TestAbbreviatePathUsesDisplayWidthForWideCharacters(t *testing.T) {
	path := "/home/user/全球化/终端设计/多语言工作区/实施版"

	got := abbreviatePath(path, 18)
	if displayWidth(got) > 18 {
		t.Fatalf("abbreviatePath() width = %d, want <= 18: %q", displayWidth(got), got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("abbreviatePath() = %q, want display-width-aware truncation", got)
	}
}

func TestReadTTYPromptEscape(t *testing.T) {
	t.Run("cancel on bare escape", func(t *testing.T) {
		action, err := readTTYPromptEscape(bufio.NewReader(strings.NewReader("")))
		if err != nil {
			t.Fatalf("readTTYPromptEscape() error = %v", err)
		}
		if action != promptEscapeCancel {
			t.Fatalf("action = %v, want %v", action, promptEscapeCancel)
		}
	})

	t.Run("history up", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("[A"))
		if _, err := reader.Peek(2); err != nil {
			t.Fatalf("Peek() error = %v", err)
		}
		action, err := readTTYPromptEscape(reader)
		if err != nil {
			t.Fatalf("readTTYPromptEscape() error = %v", err)
		}
		if action != promptEscapeHistoryUp {
			t.Fatalf("action = %v, want %v", action, promptEscapeHistoryUp)
		}
	})

	t.Run("history down", func(t *testing.T) {
		reader := bufio.NewReader(strings.NewReader("[B"))
		if _, err := reader.Peek(2); err != nil {
			t.Fatalf("Peek() error = %v", err)
		}
		action, err := readTTYPromptEscape(reader)
		if err != nil {
			t.Fatalf("readTTYPromptEscape() error = %v", err)
		}
		if action != promptEscapeHistoryDown {
			t.Fatalf("action = %v, want %v", action, promptEscapeHistoryDown)
		}
	})
}

func TestPromptHistoryBuffer(t *testing.T) {
	t.Run("nil history keeps current buffer", func(t *testing.T) {
		if got := promptHistoryBuffer(nil, "draft", promptEscapeHistoryUp); got != "draft" {
			t.Fatalf("promptHistoryBuffer(nil) = %q, want %q", got, "draft")
		}
	})

	t.Run("history up and down round-trip through draft", func(t *testing.T) {
		history := NewHistory("", 10)
		if err := history.Add("first"); err != nil {
			t.Fatalf("history.Add(first) error = %v", err)
		}
		if err := history.Add("second"); err != nil {
			t.Fatalf("history.Add(second) error = %v", err)
		}

		if got := promptHistoryBuffer(history, "draft", promptEscapeHistoryUp); got != "second" {
			t.Fatalf("promptHistoryBuffer(up) = %q, want %q", got, "second")
		}
		if got := promptHistoryBuffer(history, "draft", promptEscapeHistoryUp); got != "first" {
			t.Fatalf("promptHistoryBuffer(up again) = %q, want %q", got, "first")
		}
		if got := promptHistoryBuffer(history, "ignored", promptEscapeHistoryDown); got != "second" {
			t.Fatalf("promptHistoryBuffer(down) = %q, want %q", got, "second")
		}
		if got := promptHistoryBuffer(history, "ignored", promptEscapeHistoryDown); got != "draft" {
			t.Fatalf("promptHistoryBuffer(down to draft) = %q, want %q", got, "draft")
		}
	})
}
