package audit

import (
	"testing"
)

func TestInjectionDetector_CommandInjection(t *testing.T) {
	t.Parallel()

	d := NewInjectionDetector()
	tests := []struct {
		name      string
		input     map[string]any
		wantType  string
		wantCount int
	}{
		{
			name:      "semicolon injection",
			input:     map[string]any{"command": "ls; rm -rf /"},
			wantType:  injectionTypeCommand,
			wantCount: 1,
		},
		{
			name:      "and chain",
			input:     map[string]any{"command": "echo ok && cat /etc/passwd"},
			wantType:  injectionTypeCommand,
			wantCount: 1,
		},
		{
			name:      "or chain",
			input:     map[string]any{"command": "false || cat /etc/shadow"},
			wantType:  injectionTypeCommand,
			wantCount: 1,
		},
		{
			name:      "dollar subshell",
			input:     map[string]any{"command": "echo $(whoami)"},
			wantType:  injectionTypeCommand,
			wantCount: 1,
		},
		{
			name:      "backtick execution",
			input:     map[string]any{"command": "echo `id`"},
			wantType:  injectionTypeCommand,
			wantCount: 1,
		},
		{
			name:      "pipe to dangerous command",
			input:     map[string]any{"command": "cat file | rm something"},
			wantType:  injectionTypeCommand,
			wantCount: 1,
		},
		{
			name:      "safe command",
			input:     map[string]any{"command": "go test ./..."},
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			risks := d.Scan("fs.write", tt.input)
			matched := filterByType(risks, tt.wantType)
			if len(matched) != tt.wantCount {
				if tt.wantCount == 0 && len(risks) == 0 {
					return
				}
				t.Errorf("got %d risks of type %q, want %d (all risks: %v)", len(matched), tt.wantType, tt.wantCount, risks)
			}
		})
	}
}

func TestInjectionDetector_SQLInjection(t *testing.T) {
	t.Parallel()

	d := NewInjectionDetector()
	tests := []struct {
		name      string
		input     map[string]any
		wantCount int
	}{
		{
			name:      "or 1=1",
			input:     map[string]any{"query": "SELECT * FROM users WHERE name='' OR 1=1"},
			wantCount: 1,
		},
		{
			name:      "union select",
			input:     map[string]any{"query": "1 UNION SELECT username, password FROM users"},
			wantCount: 1,
		},
		{
			name:      "drop table",
			input:     map[string]any{"query": "DROP TABLE users"},
			wantCount: 1,
		},
		{
			name:      "semicolon delete",
			input:     map[string]any{"query": "SELECT 1; DELETE FROM users"},
			wantCount: 1,
		},
		{
			name:      "safe query",
			input:     map[string]any{"query": "SELECT id, name FROM products WHERE id = 42"},
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			risks := d.Scan("db.query", tt.input)
			matched := filterByType(risks, injectionTypeSQL)
			if len(matched) < tt.wantCount {
				t.Errorf("got %d SQL risks, want at least %d (all risks: %v)", len(matched), tt.wantCount, risks)
			}
		})
	}
}

func TestInjectionDetector_PathInjection(t *testing.T) {
	t.Parallel()

	d := NewInjectionDetector()
	tests := []struct {
		name      string
		input     map[string]any
		wantCount int
	}{
		{
			name:      "dot dot traversal",
			input:     map[string]any{"path": "../../etc/passwd"},
			wantCount: 1,
		},
		{
			name:      "null byte encoded",
			input:     map[string]any{"path": "file.txt%00.png"},
			wantCount: 1,
		},
		{
			name:      "safe path",
			input:     map[string]any{"path": "src/main.go"},
			wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			risks := d.Scan("file.read", tt.input)
			matched := filterByType(risks, injectionTypePath)
			if len(matched) != tt.wantCount {
				t.Errorf("got %d path risks, want %d (all risks: %v)", len(matched), tt.wantCount, risks)
			}
		})
	}
}

func TestInjectionDetector_EmptyInput(t *testing.T) {
	t.Parallel()

	d := NewInjectionDetector()
	risks := d.Scan("tool", nil)
	if len(risks) != 0 {
		t.Errorf("expected no risks for nil input, got %d", len(risks))
	}

	risks = d.Scan("tool", map[string]any{})
	if len(risks) != 0 {
		t.Errorf("expected no risks for empty input, got %d", len(risks))
	}
}

func TestInjectionDetector_NonStringValuesIgnored(t *testing.T) {
	t.Parallel()

	d := NewInjectionDetector()
	risks := d.Scan("tool", map[string]any{
		"count":  42,
		"flag":   true,
		"nested": map[string]any{"key": 123},
	})
	if len(risks) != 0 {
		t.Errorf("expected no risks for non-string values, got %d", len(risks))
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()

	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short, 10) = %q", got)
	}
	long := "abcdefghij"
	if got := truncate(long, 5); got != "abcde..." {
		t.Errorf("truncate(%q, 5) = %q", long, got)
	}
}

func filterByType(risks []InjectionRisk, riskType string) []InjectionRisk {
	var out []InjectionRisk
	for _, r := range risks {
		if r.Type == riskType {
			out = append(out, r)
		}
	}
	return out
}
