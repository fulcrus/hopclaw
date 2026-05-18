package knowledge

import "testing"

func TestSupportedSourceKindsExposeMetadata(t *testing.T) {
	t.Parallel()

	items := SupportedSourceKinds()
	if len(items) == 0 {
		t.Fatal("SupportedSourceKinds() returned no items")
	}

	var notion SourceKindDescriptor
	found := false
	for _, item := range items {
		if item.Kind != SourceKindNotion {
			continue
		}
		notion = item
		found = true
		break
	}
	if !found {
		t.Fatal("expected notion source kind descriptor")
	}
	if notion.Label != "Notion" {
		t.Fatalf("Label = %q, want %q", notion.Label, "Notion")
	}
	if notion.ConnectorNote == "" {
		t.Fatal("expected connector note")
	}
	if len(notion.SecretFields) != 1 || notion.SecretFields[0] != "token" {
		t.Fatalf("SecretFields = %#v", notion.SecretFields)
	}
	if len(notion.Fields) != 5 {
		t.Fatalf("len(Fields) = %d, want 5", len(notion.Fields))
	}
	fieldsByID := make(map[string]SourceFieldDescriptor, len(notion.Fields))
	for _, field := range notion.Fields {
		fieldsByID[field.ID] = field
	}
	if localeField := fieldsByID["locale"]; localeField.ID != "locale" || localeField.Scope != SourceFieldScopeRoot {
		t.Fatalf("locale field = %#v", localeField)
	}
	if tokenField := fieldsByID["token"]; tokenField.ID != "token" || !tokenField.Secret || !tokenField.Required {
		t.Fatalf("token field = %#v", tokenField)
	}
	if pageIDsField := fieldsByID["page_ids"]; pageIDsField.ID != "page_ids" || len(pageIDsField.Aliases) != 1 || pageIDsField.Aliases[0] != "page_urls" {
		t.Fatalf("page_ids field = %#v", pageIDsField)
	}
}

func TestSupportedSourceKindsCloneNestedMetadata(t *testing.T) {
	t.Parallel()

	items := SupportedSourceKinds()
	if len(items) == 0 {
		t.Fatal("SupportedSourceKinds() returned no items")
	}

	var feishu *SourceKindDescriptor
	for i := range items {
		if items[i].Kind == SourceKindFeishuDocs {
			feishu = &items[i]
			break
		}
	}
	if feishu == nil {
		t.Fatal("expected feishu source kind descriptor")
	}

	feishu.SecretFields[0] = "mutated"
	documentIDsIdx := -1
	for i, field := range feishu.Fields {
		if field.ID == "document_ids" {
			documentIDsIdx = i
			break
		}
	}
	if documentIDsIdx < 0 {
		t.Fatalf("document_ids field not found in %#v", feishu.Fields)
	}
	feishu.Fields[documentIDsIdx].Aliases = append(feishu.Fields[documentIDsIdx].Aliases, "mutated")
	if len(feishu.Requirements) > 0 && len(feishu.Requirements[0].AnyOf) > 0 && len(feishu.Requirements[0].AnyOf[0]) > 0 {
		feishu.Requirements[0].AnyOf[0][0] = "mutated"
	}

	again := SupportedSourceKinds()
	var feishuAgain *SourceKindDescriptor
	for i := range again {
		if again[i].Kind == SourceKindFeishuDocs {
			feishuAgain = &again[i]
			break
		}
	}
	if feishuAgain == nil {
		t.Fatal("expected feishu source kind descriptor on second read")
	}
	if feishuAgain.SecretFields[0] == "mutated" {
		t.Fatal("expected secret fields to be cloned")
	}
	documentIDsIdx = -1
	for i, field := range feishuAgain.Fields {
		if field.ID == "document_ids" {
			documentIDsIdx = i
			break
		}
	}
	if documentIDsIdx < 0 {
		t.Fatalf("document_ids field not found on second read: %#v", feishuAgain.Fields)
	}
	for _, alias := range feishuAgain.Fields[documentIDsIdx].Aliases {
		if alias == "mutated" {
			t.Fatal("expected field aliases to be cloned")
		}
	}
	if len(feishuAgain.Requirements) > 0 {
		for _, group := range feishuAgain.Requirements[0].AnyOf {
			for _, field := range group {
				if field == "mutated" {
					t.Fatal("expected requirement groups to be cloned")
				}
			}
		}
	}
}

func TestNormalizeSourceConfigSplitsStructuredRefs(t *testing.T) {
	t.Parallel()

	cfg := NormalizeSourceConfig(SourceKindNotion, map[string]any{
		"base_url":  "https://api.notion.com/ ",
		"page_ids":  []string{"page-1", " https://notion.so/page-2 "},
		"page_urls": []any{"https://notion.so/page-3", "page-4"},
	})
	if got := cfg["base_url"]; got != "https://api.notion.com" {
		t.Fatalf("base_url = %#v", got)
	}
	if got := cfg["page_ids"]; !equalStrings(got, []string{"page-1", "page-4"}) {
		t.Fatalf("page_ids = %#v", got)
	}
	if got := cfg["page_urls"]; !equalStrings(got, []string{"https://notion.so/page-2", "https://notion.so/page-3"}) {
		t.Fatalf("page_urls = %#v", got)
	}
}

func TestNormalizeSourceReenablesBlockedSource(t *testing.T) {
	t.Parallel()

	source, err := NormalizeSource(Source{
		Name:    "Ops Docs",
		Kind:    SourceKindLocalDir,
		Enabled: true,
		Path:    "/tmp/docs",
		Status:  SourceStatusBlocked,
	})
	if err != nil {
		t.Fatalf("NormalizeSource() error = %v", err)
	}
	if source.Status != SourceStatusReady {
		t.Fatalf("Status = %q, want %q", source.Status, SourceStatusReady)
	}
	if source.ConnectorNote == "" {
		t.Fatal("expected connector note")
	}
}

func TestNormalizeSourceEnforcesConnectorAuthCombinations(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeSource(Source{
		Name:    "Feishu Docs",
		Kind:    SourceKindFeishuDocs,
		Enabled: true,
		Config: map[string]any{
			"app_id":       "cli_xxx",
			"document_ids": []string{"doccn123"},
		},
	}); err == nil || err.Error() != "feishu docs source requires tenant_access_token or app_id/app_secret" {
		t.Fatalf("feishu NormalizeSource() error = %v", err)
	}

	if _, err := NormalizeSource(Source{
		Name:    "Confluence Docs",
		Kind:    SourceKindConfluence,
		Enabled: true,
		Config: map[string]any{
			"base_url":  "https://company.atlassian.net/wiki",
			"api_token": "token-only",
			"page_ids":  []string{"12345"},
		},
	}); err == nil || err.Error() != "confluence source requires token or email with api_token/password" {
		t.Fatalf("confluence NormalizeSource() error = %v", err)
	}
}

func TestDefaultConnectorsCoverSupportedKinds(t *testing.T) {
	t.Parallel()

	connectors := DefaultConnectors()
	items := SupportedSourceKinds()
	if len(connectors) != len(items) {
		t.Fatalf("len(connectors) = %d, want %d", len(connectors), len(items))
	}
	for _, item := range items {
		if connectors[item.Kind] == nil {
			t.Fatalf("missing connector for %q", item.Kind)
		}
	}
}

func equalStrings(got any, want []string) bool {
	typed, ok := got.([]string)
	if !ok || len(typed) != len(want) {
		return false
	}
	for i := range typed {
		if typed[i] != want[i] {
			return false
		}
	}
	return true
}
