package agent

import "testing"

func TestDetectFast_ExplicitCommands(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		wantCmds []string
	}{
		{
			name:     "backtick openssl",
			message:  "Generate a key with `openssl`",
			wantCmds: []string{"openssl"},
		},
		{
			name:     "double-quoted curl",
			message:  `Fetch using "curl" please`,
			wantCmds: []string{"curl"},
		},
		{
			name:     "single-quoted wget",
			message:  "Download with 'wget'",
			wantCmds: []string{"wget"},
		},
		{
			name:     "standalone line curl",
			message:  "Run this:\ncurl https://example.com/api",
			wantCmds: []string{"curl"},
		},
		{
			name:     "no explicit command",
			message:  "Generate a random password",
			wantCmds: nil,
		},
		{
			name:     "multiple commands via backtick",
			message:  "Use `curl` to fetch data, then `jq` to parse it",
			wantCmds: []string{"curl", "jq"},
		},
		{
			name:     "user language keywords do NOT trigger",
			message:  "Use openssl to generate a random number",
			wantCmds: nil,
		},
		{
			name:     "chinese cue phrase does NOT trigger without structure",
			message:  "请用 curl 请求这个接口",
			wantCmds: nil,
		},
		{
			name:     "backtick command in non-english message",
			message:  "Puedes usar `wget` para descargar ese archivo",
			wantCmds: []string{"wget"},
		},
		{
			name:     "standalone shell command line",
			message:  "Usa este comando:\ncurl https://example.com/api",
			wantCmds: []string{"curl"},
		},
		{
			name:     "message type is always action",
			message:  "What is the meaning of life?",
			wantCmds: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := DetectFast(tt.message)

			if len(intent.ExplicitCommands) != len(tt.wantCmds) {
				t.Errorf("ExplicitCommands count = %d, want %d",
					len(intent.ExplicitCommands), len(tt.wantCmds))
				t.Logf("Got: %v", intent.ExplicitCommands)
				return
			}

			for i, want := range tt.wantCmds {
				if intent.ExplicitCommands[i] != want {
					t.Errorf("ExplicitCommands[%d] = %q, want %q",
						i, intent.ExplicitCommands[i], want)
				}
			}

			// DetectFast always returns MessageTypeAction and RequiresCurrentInfo=false.
			if intent.MessageType != MessageTypeAction {
				t.Errorf("MessageType = %q, want %q", intent.MessageType, MessageTypeAction)
			}
			if intent.RequiresCurrentInfo {
				t.Error("RequiresCurrentInfo = true, want false")
			}
		})
	}
}

func TestToolIntent_HasExplicit(t *testing.T) {
	intent := ToolIntent{
		ExplicitCommands: []string{"openssl", "curl"},
	}

	tests := []struct {
		cmd  string
		want bool
	}{
		{"openssl rand -hex 16", true},
		{"curl https://example.com", true},
		{"wget https://example.com", false},
		{"sha256sum file.txt", false},
	}

	for _, tt := range tests {
		if got := intent.HasExplicit(tt.cmd); got != tt.want {
			t.Errorf("HasExplicit(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestToolIntent_ShouldSuggestAlternatives(t *testing.T) {
	tests := []struct {
		name   string
		intent ToolIntent
		want   bool
	}{
		{
			name:   "no explicit commands",
			intent: ToolIntent{ExplicitCommands: nil},
			want:   true,
		},
		{
			name:   "has explicit commands",
			intent: ToolIntent{ExplicitCommands: []string{"openssl"}},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.intent.ShouldSuggestAlternatives(); got != tt.want {
				t.Errorf("ShouldSuggestAlternatives() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectFast_NeverSetsMessageTypeOrCurrentInfo(t *testing.T) {
	t.Parallel()

	// Questions that previously triggered MessageTypeKnowledge or RequiresCurrentInfo
	// must now always get MessageTypeAction and RequiresCurrentInfo=false.
	for _, msg := range []string{
		"What's the latest Go version today?",
		"Can you check the current Node.js version and update package.json?",
		"¿Puedes revisar package.json?",
	} {
		intent := DetectFast(msg)
		if intent.MessageType != MessageTypeAction {
			t.Errorf("DetectFast(%q).MessageType = %q, want %q", msg, intent.MessageType, MessageTypeAction)
		}
		if intent.RequiresCurrentInfo {
			t.Errorf("DetectFast(%q).RequiresCurrentInfo = true, want false", msg)
		}
	}
}
