package runtime

import "testing"

func TestExtractProfileMemoryStructuredAssignments(t *testing.T) {
	t.Parallel()

	records := extractProfileMemory(`
name: Alice Example
reply_language: zh-cn
response_style: concise
`)

	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3 (%#v)", len(records), records)
	}
	if got := records[0].Field + "=" + records[0].Value; got != "name=Alice Example" {
		t.Fatalf("records[0] = %q, want name=Alice Example", got)
	}
	if got := records[1].Field + "=" + records[1].Value; got != "reply_language=zh-CN" {
		t.Fatalf("records[1] = %q, want reply_language=zh-CN", got)
	}
	if got := records[2].Field + "=" + records[2].Value; got != "response_style=concise" {
		t.Fatalf("records[2] = %q, want response_style=concise", got)
	}
}

func TestExtractProfileMemoryStructuredJSON(t *testing.T) {
	t.Parallel()

	records := extractProfileMemory(`{"display_name":"Alice","locale":"en-us","style":"detailed"}`)

	if len(records) != 3 {
		t.Fatalf("len(records) = %d, want 3 (%#v)", len(records), records)
	}
	if got := records[1].Value; got != "en-US" {
		t.Fatalf("reply_language = %q, want en-US", got)
	}
	if got := records[2].Value; got != "detailed" {
		t.Fatalf("response_style = %q, want detailed", got)
	}
}

func TestExtractProfileMemoryIgnoresNaturalLanguagePhrases(t *testing.T) {
	t.Parallel()

	records := extractProfileMemory("请用中文回复，尽量简洁一些，我叫小王。")
	if len(records) != 0 {
		t.Fatalf("extractProfileMemory(natural language) = %#v, want no records", records)
	}

	records = extractProfileMemory("Please reply in English with more detail. Call me Alice.")
	if len(records) != 0 {
		t.Fatalf("extractProfileMemory(english natural language) = %#v, want no records", records)
	}
}

func TestExtractWorkspaceAndProjectMemoryUsesPathsNotKeywords(t *testing.T) {
	t.Parallel()

	records := extractWorkspaceAndProjectMemory("chat-memory", "repo hopclaw needs review")
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1 (%#v)", len(records), records)
	}
	if got := records[0].Field + "=" + records[0].Value; got != "session_key=chat-memory" {
		t.Fatalf("records[0] = %q, want session_key=chat-memory", got)
	}
}
