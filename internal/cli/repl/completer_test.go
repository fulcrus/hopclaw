package repl

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

func TestComposerCompleterAttachmentCandidates(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(filePath, []byte("name: hopclaw\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", filePath, err)
	}
	subdir := filepath.Join(dir, "internal")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", subdir, err)
	}

	completer := &composerCompleter{cwd: dir, root: dir, registry: NewCommandRegistry()}
	items := completer.AttachmentCandidates("conf")
	if len(items) == 0 {
		t.Fatal("expected attachment candidates")
	}
	if items[0].Kind != richedit.TokenFile {
		t.Fatalf("items[0].Kind = %v, want %v", items[0].Kind, richedit.TokenFile)
	}
	if items[0].Label != "config.yaml" {
		t.Fatalf("items[0].Label = %q, want %q", items[0].Label, "config.yaml")
	}
}

func TestComposerCompleterCompletePathPreservesRelativePrefix(t *testing.T) {
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q) error = %v", dir, err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	completer := &composerCompleter{cwd: dir, root: dir, registry: NewCommandRegistry()}
	got, ok := completer.CompletePath("./inte")
	if !ok {
		t.Fatal("CompletePath() did not return a completion")
	}
	if got != "./internal/" {
		t.Fatalf("CompletePath() = %q, want %q", got, "./internal/")
	}
}

func TestComposerCompleterCompleteSlashModelUsesModelChoices(t *testing.T) {
	completer := &composerCompleter{
		registry: NewCommandRegistry(),
		modelNamesFn: func() []string {
			return []string{"gpt-5.4", "gpt-5.4-mini"}
		},
	}

	got, ok := completer.CompleteSlash("/model ", "gpt-5.4-m")
	if !ok {
		t.Fatal("CompleteSlash() did not return a model completion")
	}
	if got != "gpt-5.4-mini" {
		t.Fatalf("CompleteSlash() = %q, want %q", got, "gpt-5.4-mini")
	}
}

func TestComposerCompleterCompleteSlashAttachType(t *testing.T) {
	completer := &composerCompleter{registry: NewCommandRegistry()}

	got, ok := completer.CompleteSlash("/attach ", "im")
	if !ok {
		t.Fatal("CompleteSlash() did not return an attach-type completion")
	}
	if got != "image" {
		t.Fatalf("CompleteSlash() = %q, want %q", got, "image")
	}
}

func TestComposerCompleterCompleteSlashToolsSubcommand(t *testing.T) {
	completer := &composerCompleter{registry: NewCommandRegistry()}

	got, ok := completer.CompleteSlash("/tools ", "se")
	if !ok {
		t.Fatal("CompleteSlash() did not return a tools subcommand completion")
	}
	if got != "search" {
		t.Fatalf("CompleteSlash() = %q, want %q", got, "search")
	}
}

func TestComposerCompleterCompleteSlashSkillsSubcommand(t *testing.T) {
	completer := &composerCompleter{registry: NewCommandRegistry()}

	got, ok := completer.CompleteSlash("/skills ", "ins")
	if !ok {
		t.Fatal("CompleteSlash() did not return a skills subcommand completion")
	}
	if got != "install" {
		t.Fatalf("CompleteSlash() = %q, want %q", got, "install")
	}
}

func TestHistoryRichDraftRoundTrip(t *testing.T) {
	history := NewHistory("", 10)
	doc := richedit.NewDocument()
	doc.InsertRune(0, 0, 'A')
	doc.InsertImage(0, 1, "data:image/png;base64,abc", "image/png", "")
	snap := doc.Snapshot(richedit.Cursor{Line: 0, Col: 2})

	if err := history.AddDraft(snap); err != nil {
		t.Fatalf("AddDraft() error = %v", err)
	}

	prev, ok := history.PreviousDraft(richedit.NewDocument().Snapshot(richedit.Cursor{}))
	if !ok {
		t.Fatal("PreviousDraft() should restore the stored draft")
	}
	restored := richedit.NewDocument()
	restored.Restore(prev)
	if got := restored.Text(); got != "A[IMAGE#1]" {
		t.Fatalf("restored.Text() = %q, want %q", got, "A[IMAGE#1]")
	}

	next, ok := history.NextDraft()
	if ok {
		// The stored in-progress draft should be the empty document we passed in.
		empty := richedit.NewDocument()
		restored.Restore(next)
		if restored.Text() != empty.Text() {
			t.Fatalf("NextDraft() should return the saved in-progress draft, got %q", restored.Text())
		}
	}
}
