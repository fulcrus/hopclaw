package skill

import "testing"

func TestNormalizeSideEffectClass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"", "read"},
		{"read", "read"},
		{"READ", "read"},
		{" Read ", "read"},
		{"readonly", "read"},
		{"local_write", "local_write"},
		{"external_write", "external_write"},
		{"remote_write", "external_write"},
		{"destructive", "destructive"},
		{"unknown_value", "destructive"},
		{"  DESTRUCTIVE  ", "destructive"},
	}
	for _, tt := range tests {
		got := NormalizeSideEffectClass(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeSideEffectClass(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRuntimeBoundaryForSideEffect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input             string
		wantClass         SideEffectClass
		wantFSWrite       bool
		wantProcess       bool
		wantExternalRead  bool
		wantExternalWrite bool
	}{
		{"read", SideEffectRead, false, false, true, false},
		{"local_write", SideEffectLocalWrite, true, false, true, false},
		{"remote_write", SideEffectExternalWrite, false, false, true, true},
		{"destructive", SideEffectDestructive, true, true, true, true},
		{"garbage", SideEffectDestructive, true, true, true, true},
	}
	for _, tt := range tests {
		got := RuntimeBoundaryForSideEffect(tt.input)
		if got.Class != tt.wantClass ||
			got.AllowsFilesystemWrite != tt.wantFSWrite ||
			got.AllowsProcessSpawn != tt.wantProcess ||
			got.AllowsExternalNetworkRead != tt.wantExternalRead ||
			got.AllowsExternalNetworkWrite != tt.wantExternalWrite {
			t.Fatalf("RuntimeBoundaryForSideEffect(%q) = %+v", tt.input, got)
		}
	}
}
