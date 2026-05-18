package i18n

import (
	"os"
	"testing"
)

// Tests that modify environment variables cannot use t.Parallel()
// because os.Setenv mutates global process state.

func TestDetectLocale_HopClawLocale(t *testing.T) {
	save := setEnvVars(t, map[string]string{
		"HOPCLAW_LOCALE": "zh-CN",
		"LC_ALL":         "en_US.UTF-8",
		"LANG":           "en_US.UTF-8",
	})
	defer save()

	got := DetectLocale()
	if got != ZhCN {
		t.Fatalf("expected %q, got %q", ZhCN, got)
	}
}

func TestDetectLocale_LcAll(t *testing.T) {
	save := setEnvVars(t, map[string]string{
		"HOPCLAW_LOCALE": "",
		"LC_ALL":         "zh_TW.UTF-8",
		"LANG":           "en_US.UTF-8",
	})
	defer save()

	got := DetectLocale()
	if got != ZhTW {
		t.Fatalf("expected %q, got %q", ZhTW, got)
	}
}

func TestDetectLocale_Lang(t *testing.T) {
	save := setEnvVars(t, map[string]string{
		"HOPCLAW_LOCALE": "",
		"LC_ALL":         "",
		"LANG":           "zh_CN.UTF-8",
	})
	defer save()

	got := DetectLocale()
	if got != ZhCN {
		t.Fatalf("expected %q, got %q", ZhCN, got)
	}
}

func TestDetectLocale_NoEnvVars(t *testing.T) {
	save := setEnvVars(t, map[string]string{
		"HOPCLAW_LOCALE": "",
		"LC_ALL":         "",
		"LANG":           "",
	})
	defer save()

	got := DetectLocale()
	if got != EN {
		t.Fatalf("expected %q, got %q", EN, got)
	}
}

func TestNormalizeLocale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  Locale
	}{
		{name: "zh_CN.UTF-8", input: "zh_CN.UTF-8", want: ZhCN},
		{name: "zh-CN", input: "zh-CN", want: ZhCN},
		{name: "zh_TW.UTF-8", input: "zh_TW.UTF-8", want: ZhTW},
		{name: "zh-TW", input: "zh-TW", want: ZhTW},
		{name: "zh_HK.UTF-8", input: "zh_HK.UTF-8", want: ZhTW},
		{name: "zh-Hans", input: "zh-Hans", want: ZhCN},
		{name: "zh-Hant", input: "zh-Hant", want: ZhTW},
		{name: "zh bare", input: "zh", want: ZhCN},
		{name: "en_US.UTF-8", input: "en_US.UTF-8", want: EN},
		{name: "en-GB", input: "en-GB", want: EN},
		{name: "en bare", input: "en", want: EN},
		{name: "unknown locale", input: "fr_FR.UTF-8", want: EN},
		{name: "empty string", input: "", want: EN},
		{name: "ja_JP.UTF-8", input: "ja_JP.UTF-8", want: JaJP},
		{name: "ja-JP", input: "ja-JP", want: JaJP},
		{name: "ja bare", input: "ja", want: JaJP},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeLocale(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeLocale(%q): expected %q, got %q", tc.input, tc.want, got)
			}
		})
	}
}

// setEnvVars sets environment variables and returns a restore function.
// Tests using this helper must NOT call t.Parallel() because os.Setenv
// mutates global process state.
func setEnvVars(t *testing.T, vars map[string]string) func() {
	t.Helper()

	type envEntry struct {
		value string
		isSet bool
	}
	saved := make(map[string]envEntry, len(vars))

	for k := range vars {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = envEntry{value: v, isSet: true}
		} else {
			saved[k] = envEntry{isSet: false}
		}
	}

	for k, v := range vars {
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}

	return func() {
		for k, entry := range saved {
			if entry.isSet {
				os.Setenv(k, entry.value)
			} else {
				os.Unsetenv(k)
			}
		}
	}
}
