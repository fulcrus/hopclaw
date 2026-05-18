package toolruntime

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

func TestLocalExecRunsCompanionManifestTool(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "writer")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	script := `#!/bin/sh
read input
printf '{"content":"tool ok","artifact_uri":"artifact://result/1"}'
`
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(run.sh) error = %v", err)
	}

	exec := NewLocalExec(LocalExecConfig{
		InjectedEnvResolver: func(pkg *skill.SkillPackage) (map[string]string, error) {
			if pkg == nil || pkg.Name() != "writer" {
				return nil, nil
			}
			return map[string]string{"INJECTED_TOKEN": "secret-value"}, nil
		},
	})
	session := &agent.Session{
		ID: "sess-1",
		Session: contextengine.Session{
			SkillSnapshot: skill.SessionSkillSnapshot{
				Fingerprint: "skills-1",
				Ordered: []skill.BoundSkill{{
					Package: &skill.SkillPackage{
						Source: skill.SkillSource{Dir: skillDir},
						Prompt: skill.PromptSkill{Name: "writer", Description: "write files"},
						ToolManifests: []skill.ToolManifest{{
							Name: "fs.write",
							Runtime: skill.ToolRuntimeSpec{
								Entry: "scripts/run.sh",
								Shell: "sh",
							},
						}},
					},
					Eligibility: skill.EligibilityResult{Eligible: true},
				}},
			},
		},
	}
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{
		ID: "run-1",
	}, session, []agent.ToolCall{{
		ID:   "call-1",
		Name: "fs.write",
		Input: map[string]any{
			"path": "README.md",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].Content != "tool ok" {
		t.Fatalf("results[0].Content = %q", results[0].Content)
	}
	if results[0].ArtifactURI != "artifact://result/1" {
		t.Fatalf("results[0].ArtifactURI = %q", results[0].ArtifactURI)
	}
}

func TestLocalExecRunsFeishuBundleResolver(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	root := filepath.Join(filepath.Dir(file), "..")
	bundleDir := filepath.Join(root, "bundles", "feishu-suite")
	entry := filepath.Join(bundleDir, "runtime", "feishu_suite.py")
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("feishu bundle runtime missing: %v", err)
	}

	exec := NewLocalExec(LocalExecConfig{
		InjectedEnvResolver: func(pkg *skill.SkillPackage) (map[string]string, error) {
			if pkg == nil || pkg.Name() != "writer" {
				return nil, nil
			}
			return map[string]string{"INJECTED_TOKEN": "secret-value"}, nil
		},
	})
	session := &agent.Session{
		ID: "sess-feishu",
		Session: contextengine.Session{
			SkillSnapshot: skill.SessionSkillSnapshot{
				Fingerprint: "skills-feishu",
				Ordered: []skill.BoundSkill{{
					Package: &skill.SkillPackage{
						Source: skill.SkillSource{Dir: bundleDir},
						Prompt: skill.PromptSkill{Name: "feishu-suite", Description: "Feishu bundle"},
						ToolManifests: []skill.ToolManifest{{
							Name: "feishu.url.resolve",
							Runtime: skill.ToolRuntimeSpec{
								Entry: "runtime/feishu_suite.py",
							},
						}},
					},
					Eligibility: skill.EligibilityResult{Eligible: true},
				}},
			},
		},
	}

	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{
		ID: "run-feishu",
	}, session, []agent.ToolCall{{
		ID:   "call-feishu",
		Name: "feishu.url.resolve",
		Input: map[string]any{
			"url": "https://feishu.cn/docx/ABCD1234",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if !strings.Contains(results[0].Content, "Resolved Feishu URL as docx") {
		t.Fatalf("unexpected content: %q", results[0].Content)
	}
}

func TestLocalExecInjectsManagedEnv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	skillDir := filepath.Join(root, "writer")
	if err := os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	script := `#!/bin/sh
printf '{"content":"%s"}' "${INJECTED_TOKEN}"
`
	if err := os.WriteFile(filepath.Join(skillDir, "scripts", "run.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(run.sh) error = %v", err)
	}

	exec := NewLocalExec(LocalExecConfig{
		InjectedEnvResolver: func(pkg *skill.SkillPackage) (map[string]string, error) {
			if pkg == nil || pkg.Name() != "writer" {
				return nil, nil
			}
			return map[string]string{"INJECTED_TOKEN": "secret-value"}, nil
		},
	})
	session := &agent.Session{
		ID: "sess-env",
		Session: contextengine.Session{
			SkillSnapshot: skill.SessionSkillSnapshot{
				Fingerprint: "skills-env",
				Ordered: []skill.BoundSkill{{
					Package: &skill.SkillPackage{
						Source: skill.SkillSource{Dir: skillDir},
						Prompt: skill.PromptSkill{Name: "writer", Description: "write files"},
						ToolManifests: []skill.ToolManifest{{
							Name: "env.read",
							Runtime: skill.ToolRuntimeSpec{
								Entry: "scripts/run.sh",
								Shell: "sh",
							},
						}},
					},
					Eligibility: skill.EligibilityResult{
						Eligible:    true,
						InjectedEnv: []string{"INJECTED_TOKEN"},
					},
				}},
			},
		},
	}
	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-env"}, session, []agent.ToolCall{{
		ID:    "call-env",
		Name:  "env.read",
		Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].Content != "secret-value" {
		t.Fatalf("results[0].Content = %q", results[0].Content)
	}
}
