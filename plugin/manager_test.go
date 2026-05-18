package plugin

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestManagerOutputsStableOrderAcrossRegistrationOrder(t *testing.T) {
	t.Parallel()

	manager := NewManager()
	if err := manager.Register(LoadedPlugin{
		Dir: filepath.Join(t.TempDir(), "zeta"),
		Manifest: Manifest{
			Name:      "zeta",
			SkillsDir: "skills",
			HooksDir:  "hooks",
			Commands: []CommandDecl{
				{Name: "inspect", Exec: "./bin/zeta-inspect"},
			},
			Tools: []ToolDecl{
				{Name: "zeta.echo"},
			},
		},
	}); err != nil {
		t.Fatalf("Register(zeta) error = %v", err)
	}
	if err := manager.Register(LoadedPlugin{
		Dir: filepath.Join(t.TempDir(), "alpha"),
		Manifest: Manifest{
			Name:      "alpha",
			SkillsDir: "skills",
			HooksDir:  "hooks",
			Commands: []CommandDecl{
				{Name: "inspect", Exec: "./bin/alpha-inspect"},
			},
			Tools: []ToolDecl{
				{Name: "alpha.echo"},
			},
		},
	}); err != nil {
		t.Fatalf("Register(alpha) error = %v", err)
	}

	toolNames := make([]string, 0, len(manager.Tools()))
	for _, tool := range manager.Tools() {
		toolNames = append(toolNames, tool.Name)
	}
	if want := []string{"alpha.echo", "zeta.echo"}; !reflect.DeepEqual(toolNames, want) {
		t.Fatalf("Tools() = %v, want %v", toolNames, want)
	}

	skillDirs := manager.SkillDirs()
	if len(skillDirs) != 2 {
		t.Fatalf("SkillDirs() = %v", skillDirs)
	}
	if filepath.Base(filepath.Dir(skillDirs[0])) != "alpha" || filepath.Base(filepath.Dir(skillDirs[1])) != "zeta" {
		t.Fatalf("SkillDirs() order = %v", skillDirs)
	}

	hookDirs := manager.HookDirs()
	if len(hookDirs) != 2 {
		t.Fatalf("HookDirs() = %v", hookDirs)
	}
	if filepath.Base(filepath.Dir(hookDirs[0])) != "alpha" || filepath.Base(filepath.Dir(hookDirs[1])) != "zeta" {
		t.Fatalf("HookDirs() order = %v", hookDirs)
	}

	commands := manager.Commands()
	if len(commands) != 2 {
		t.Fatalf("len(Commands()) = %d, want 2", len(commands))
	}
	if commands["alpha/inspect"].Exec != "./bin/alpha-inspect" {
		t.Fatalf("Commands()[alpha/inspect] = %#v", commands["alpha/inspect"])
	}
	if commands["zeta/inspect"].Exec != "./bin/zeta-inspect" {
		t.Fatalf("Commands()[zeta/inspect] = %#v", commands["zeta/inspect"])
	}
}
