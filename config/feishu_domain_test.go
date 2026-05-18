package config

import "testing"

func TestNormalizeFeishuDomainCanonicalizesKnownBaseURLs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "builtin feishu", input: "feishu", want: "feishu"},
		{name: "builtin lark", input: "lark", want: "lark"},
		{name: "feishu open apis path", input: "https://open.feishu.cn/open-apis", want: "https://open.feishu.cn"},
		{name: "lark open apis path slash", input: "https://open.larksuite.com/open-apis/", want: "https://open.larksuite.com"},
		{name: "custom host path preserved", input: "https://example.com/custom", want: "https://example.com/custom"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeFeishuDomain(tc.input); got != tc.want {
				t.Fatalf("normalizeFeishuDomain(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
