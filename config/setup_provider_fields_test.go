package config

import "testing"

func TestSplitSetupProviderFieldList(t *testing.T) {
	t.Parallel()

	got := SplitSetupProviderFieldList(" primary-key \nbackup-key, third-key \n")
	want := []string{"primary-key", "backup-key", "third-key"}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d (%#v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseSetupProviderFieldMap(t *testing.T) {
	t.Parallel()

	got, err := ParseSetupProviderFieldMap(" Authorization: Bearer demo \nX-Trace-Id: trace-123\n")
	if err != nil {
		t.Fatalf("ParseSetupProviderFieldMap() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got["Authorization"] != "Bearer demo" {
		t.Fatalf("Authorization = %q, want %q", got["Authorization"], "Bearer demo")
	}
	if got["X-Trace-Id"] != "trace-123" {
		t.Fatalf("X-Trace-Id = %q, want %q", got["X-Trace-Id"], "trace-123")
	}
}

func TestParseSetupProviderFieldMapRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	if _, err := ParseSetupProviderFieldMap("not-a-valid-map-line"); err == nil {
		t.Fatal("expected invalid setup provider map entry to fail")
	}
}
