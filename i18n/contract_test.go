package i18n

import (
	"reflect"
	"testing"
)

func TestSupportedLocaleStringsStable(t *testing.T) {
	t.Parallel()

	want := []string{"en", "zh-CN", "zh-TW", "ja-JP"}
	if got := SupportedLocaleStrings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SupportedLocaleStrings() = %#v, want %#v", got, want)
	}
}
