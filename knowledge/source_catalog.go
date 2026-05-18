package knowledge

import (
	"fmt"
	"strings"
)

// SourceKindDescriptor describes a supported knowledge source kind for
// operator surfaces, UI metadata, and configuration validation.
type SourceFieldType string

const (
	SourceFieldTypeString     SourceFieldType = "string"
	SourceFieldTypeStringList SourceFieldType = "string_list"
	SourceFieldTypeBoolean    SourceFieldType = "boolean"
)

type SourceFieldScope string

const (
	SourceFieldScopeRoot   SourceFieldScope = "root"
	SourceFieldScopeConfig SourceFieldScope = "config"
)

type SourceFieldDescriptor struct {
	ID           string           `json:"id"`
	Scope        SourceFieldScope `json:"scope,omitempty"`
	Key          string           `json:"key"`
	Label        string           `json:"label"`
	Description  string           `json:"description,omitempty"`
	Type         SourceFieldType  `json:"type,omitempty"`
	Required     bool             `json:"required,omitempty"`
	Secret       bool             `json:"secret,omitempty"`
	Placeholder  string           `json:"placeholder,omitempty"`
	DefaultValue any              `json:"default_value,omitempty"`
	Rows         int              `json:"rows,omitempty"`
	Aliases      []string         `json:"aliases,omitempty"`
}

type SourceRequirement struct {
	AnyOf       [][]string `json:"any_of,omitempty"`
	Description string     `json:"description,omitempty"`
}

type SourceKindDescriptor struct {
	Kind          SourceKind              `json:"kind"`
	Label         string                  `json:"label"`
	ConnectorNote string                  `json:"connector_note,omitempty"`
	SecretFields  []string                `json:"secret_fields,omitempty"`
	Fields        []SourceFieldDescriptor `json:"fields,omitempty"`
	Requirements  []SourceRequirement     `json:"requirements,omitempty"`
}

type sourceKindSpec struct {
	descriptor      SourceKindDescriptor
	newConnector    func() Connector
	normalizeConfig func(map[string]any) map[string]any
	validate        func(Source) error
}

var sourceKindSpecs = []sourceKindSpec{
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindLocalDir,
			Label:         "Local Directory",
			ConnectorNote: "Indexes a maintained local folder without moving the source of truth.",
			Fields: []SourceFieldDescriptor{
				rootStringField("path", "Directory Path", "/docs/operations", "Absolute path to the maintained folder HopClaw should index.", true),
				rootStringListField("include_globs", "Include Globs", "docs/**\n*.md", "Optional allowlist. Leave empty to scan the full directory.", false, 4),
				rootStringListField("exclude_globs", "Exclude Globs", ".git/**\nnode_modules/**", "Optional denylist for generated, vendored, or irrelevant files.", false, 4),
			},
		},
		newConnector: func() Connector { return &LocalDirConnector{} },
		validate: func(source Source) error {
			if strings.TrimSpace(source.Path) == "" {
				return fmt.Errorf("path is required for this source kind")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindGitRepo,
			Label:         "Git Repository",
			ConnectorNote: "Indexes a checked-out repository so docs, code, and runbooks stay external-first.",
			Fields: []SourceFieldDescriptor{
				rootStringField("path", "Repository Path", "/repos/team-handbook", "Path to an already checked-out repository on disk.", true),
				rootStringListField("include_globs", "Include Globs", "docs/**\n*.md", "Optional allowlist for the repository subtree to index.", false, 4),
				rootStringListField("exclude_globs", "Exclude Globs", ".git/**\nvendor/**", "Optional denylist for generated files, vendored code, or large trees.", false, 4),
			},
		},
		newConnector: func() Connector { return &GitRepoConnector{} },
		validate: func(source Source) error {
			if strings.TrimSpace(source.Path) == "" {
				return fmt.Errorf("path is required for this source kind")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindWebURLs,
			Label:         "Web URLs",
			ConnectorNote: "Indexes published pages or docs URLs that your team already maintains elsewhere.",
			Fields: []SourceFieldDescriptor{
				rootStringListField("urls", "URLs", "https://docs.example.com/faq\nhttps://docs.example.com/runbook", "One published URL per line.", true, 6),
			},
		},
		newConnector: func() Connector { return &WebURLConnector{} },
		validate: func(source Source) error {
			if len(source.URLs) == 0 {
				return fmt.Errorf("urls are required for web url sources")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindFeishuDocs,
			Label:         "Feishu Docs",
			ConnectorNote: "Indexes Feishu documents while your team keeps editing them in Feishu.",
			SecretFields:  []string{"tenant_access_token", "app_secret"},
			Fields: []SourceFieldDescriptor{
				configStringField("base_url", "Base URL", "https://open.feishu.cn/open-apis", "Optional override for Feishu Open Platform base URL.", false),
				configStringField("app_id", "App ID", "cli_xxx", "Used together with App Secret when you want HopClaw to fetch a tenant access token.", false),
				configSecretField("tenant_access_token", "Tenant Access Token", "Enter secret value", "Use this when you already manage a tenant access token externally.", false),
				configSecretField("app_secret", "App Secret", "Enter secret value", "Required when authenticating with App ID instead of a tenant access token.", false),
				configStringListField("document_ids", "Document IDs or URLs", "doccnxxxxxxxxxxxxxxxxxxxx\nhttps://example.feishu.cn/docx/doccnxxxxxxxxxxxxxxxxxxxx", "Paste one document ID or document URL per line.", false, 5, "document_urls"),
				configStringListField("wiki_node_tokens", "Wiki Node Tokens", "Optional wiki node tokens, one per line", "Optional wiki node tokens to crawl via the wiki API.", false, 4),
			},
			Requirements: []SourceRequirement{
				{
					AnyOf: [][]string{
						{"tenant_access_token"},
						{"app_id", "app_secret"},
					},
					Description: "Provide a tenant access token or a matching app_id/app_secret pair.",
				},
				{
					AnyOf: [][]string{
						{"document_ids"},
						{"wiki_node_tokens"},
					},
					Description: "Provide document IDs/URLs or wiki node tokens to index.",
				},
			},
		},
		newConnector:    func() Connector { return &FeishuDocsConnector{} },
		normalizeConfig: normalizeFeishuDocsConfig,
		validate: func(source Source) error {
			if !hasAnySourceConfig(source.Config, "document_ids", "document_urls", "wiki_node_tokens") {
				return fmt.Errorf("feishu docs source requires document_ids, document_urls, or wiki_node_tokens")
			}
			if !hasAnySourceConfig(source.Config, "tenant_access_token") && !hasAllSourceConfig(source.Config, "app_id", "app_secret") {
				return fmt.Errorf("feishu docs source requires tenant_access_token or app_id/app_secret")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindNotion,
			Label:         "Notion",
			ConnectorNote: "Indexes Notion pages so project docs stay in Notion, but HopClaw can retrieve them during execution.",
			SecretFields:  []string{"token"},
			Fields: []SourceFieldDescriptor{
				configStringField("base_url", "Base URL", "https://api.notion.com", "Optional override for the Notion API base URL.", false),
				configStringField("notion_version", "Notion Version", "2022-06-28", "Optional Notion-Version header override.", false),
				configSecretField("token", "Integration Token", "Enter secret value", "The Notion integration token used to read pages.", true),
				configStringListField("page_ids", "Page IDs or URLs", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\nhttps://www.notion.so/workspace/Team-Handbook-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "Paste one page ID or page URL per line.", true, 6, "page_urls"),
			},
		},
		newConnector:    func() Connector { return &NotionConnector{} },
		normalizeConfig: normalizeNotionConfig,
		validate: func(source Source) error {
			if sourceConfigString(source.Config, "token") == "" {
				return fmt.Errorf("notion source requires token")
			}
			if !hasAnySourceConfig(source.Config, "page_ids", "page_urls") {
				return fmt.Errorf("notion source requires page_ids or page_urls")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindConfluence,
			Label:         "Confluence",
			ConnectorNote: "Indexes Confluence pages and descendants so internal runbooks stay in Confluence, not inside the agent UI.",
			SecretFields:  []string{"token", "api_token", "password"},
			Fields: []SourceFieldDescriptor{
				configStringField("base_url", "Base URL", "https://company.atlassian.net/wiki", "Confluence site or wiki base URL.", true),
				configStringField("email", "Email", "user@company.com", "Required when using API token or password based basic auth.", false),
				configSecretField("api_token", "API Token", "Enter secret value", "Use with Email for Atlassian API token auth.", false),
				configSecretField("password", "Password", "Enter secret value", "Use with Email for legacy basic auth when supported.", false),
				configSecretField("token", "Bearer Token", "Enter secret value", "Optional bearer token if your Confluence deployment supports it.", false),
				configStringListField("page_ids", "Page IDs or URLs", "123456789\nhttps://company.atlassian.net/wiki/spaces/OPS/pages/123456789/Runbook", "Paste one page ID or Confluence page URL per line.", true, 6, "page_urls"),
				configBooleanField("include_descendants", "Include Descendant Pages", "When enabled, HopClaw expands each seed page to its descendants.", true),
			},
			Requirements: []SourceRequirement{
				{
					AnyOf: [][]string{
						{"token"},
						{"email", "api_token"},
						{"email", "password"},
					},
					Description: "Provide a bearer token or email plus API token/password.",
				},
			},
		},
		newConnector:    func() Connector { return &ConfluenceConnector{} },
		normalizeConfig: normalizeConfluenceConfig,
		validate: func(source Source) error {
			if sourceConfigString(source.Config, "base_url") == "" {
				return fmt.Errorf("confluence source requires base_url")
			}
			if !hasAnySourceConfig(source.Config, "page_ids", "page_urls") {
				return fmt.Errorf("confluence source requires page_ids or page_urls")
			}
			hasBearer := hasAnySourceConfig(source.Config, "token")
			hasBasic := sourceConfigString(source.Config, "email") != "" && hasAnySourceConfig(source.Config, "api_token", "password")
			if !hasBearer && !hasBasic {
				return fmt.Errorf("confluence source requires token or email with api_token/password")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindGoogleDrive,
			Label:         "Google Drive / Docs",
			ConnectorNote: "Indexes Google Drive and Google Docs content while your team keeps editing in Google Workspace.",
			SecretFields:  []string{"token"},
			Fields: []SourceFieldDescriptor{
				configStringField("base_url", "Base URL", "https://www.googleapis.com", "Optional override for the Google APIs base URL.", false),
				configSecretField("token", "Access Token", "Enter secret value", "OAuth access token used to fetch Google Drive and Docs content.", true),
				configStringListField("file_ids", "File IDs or URLs", "1AbCdEfGhIjKlmn\nhttps://docs.google.com/document/d/1AbCdEfGhIjKlmn/edit", "Paste one file ID or Drive/Docs URL per line.", true, 6, "file_urls"),
				configStringField("export_mime_type", "Export MIME Type", "text/plain", "Optional MIME type used when exporting Google native docs.", false),
			},
		},
		newConnector:    func() Connector { return &GoogleDriveConnector{} },
		normalizeConfig: normalizeGoogleDriveConfig,
		validate: func(source Source) error {
			if sourceConfigString(source.Config, "token") == "" {
				return fmt.Errorf("google drive source requires token")
			}
			if !hasAnySourceConfig(source.Config, "file_ids", "file_urls") {
				return fmt.Errorf("google drive source requires file_ids or file_urls")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindYuque,
			Label:         "Yuque",
			ConnectorNote: "Indexes Yuque repos and docs so product knowledge stays maintained in Yuque, not duplicated into the agent.",
			SecretFields:  []string{"token"},
			Fields: []SourceFieldDescriptor{
				configStringField("base_url", "Base URL", "https://www.yuque.com/api/v2", "Optional override for the Yuque API base URL.", false),
				configSecretField("token", "Token", "Enter secret value", "Yuque access token used to enumerate repos and docs.", true),
				configStringListField("repo_ids", "Repo IDs", "123456\n987654", "Optional Yuque repo IDs, one per line.", false, 4),
				configStringListField("repo_paths", "Repo Paths or URLs", "team/runbook\norg/product-manual", "Optional repo paths or repo URLs, one per line.", false, 4, "repo_urls"),
				configStringListField("doc_urls", "Doc URLs", "https://www.yuque.com/team/runbook/rollback-guide", "Optional direct Yuque doc URLs, one per line.", false, 5),
				configStringListField("doc_slugs", "Doc Slugs", "rollback-guide\ndaily-ops", "Optional doc slugs to further narrow sync scope.", false, 4),
			},
			Requirements: []SourceRequirement{
				{
					AnyOf: [][]string{
						{"repo_ids"},
						{"repo_paths"},
						{"doc_urls"},
					},
					Description: "Provide repo IDs, repo paths/URLs, or direct doc URLs.",
				},
			},
		},
		newConnector:    func() Connector { return &YuqueConnector{} },
		normalizeConfig: normalizeYuqueConfig,
		validate: func(source Source) error {
			if sourceConfigString(source.Config, "token") == "" {
				return fmt.Errorf("yuque source requires token")
			}
			if !hasAnySourceConfig(source.Config, "repo_ids", "repo_paths", "repo_urls", "doc_urls") {
				return fmt.Errorf("yuque source requires repo_paths, repo_ids, repo_urls, or doc_urls")
			}
			return nil
		},
	},
	{
		descriptor: SourceKindDescriptor{
			Kind:          SourceKindTencentDocs,
			Label:         "Tencent Docs",
			ConnectorNote: "Indexes Tencent Docs exports so execution can retrieve working documents that stay managed in Tencent Docs.",
			SecretFields:  []string{"token"},
			Fields: []SourceFieldDescriptor{
				configStringField("base_url", "Base URL", "https://docs.qq.com", "Optional override for the Tencent Docs base URL.", false),
				configSecretField("token", "Token", "Enter secret value", "Access token used to export Tencent Docs content.", true),
				configStringListField("file_ids", "File IDs or URLs", "DWU123\nhttps://docs.qq.com/doc/DWU123", "Paste one file ID or document URL per line.", true, 6, "file_urls"),
				configStringField("export_type", "Export Type", "txt", "Optional Tencent Docs export type.", false),
			},
		},
		newConnector:    func() Connector { return &TencentDocsConnector{} },
		normalizeConfig: normalizeTencentDocsConfig,
		validate: func(source Source) error {
			if sourceConfigString(source.Config, "token") == "" {
				return fmt.Errorf("tencent docs source requires token")
			}
			if !hasAnySourceConfig(source.Config, "file_ids", "file_urls") {
				return fmt.Errorf("tencent docs source requires file_ids or file_urls")
			}
			return nil
		},
	},
}

var sourceKindSpecByKind = func() map[SourceKind]sourceKindSpec {
	out := make(map[SourceKind]sourceKindSpec, len(sourceKindSpecs))
	for _, spec := range sourceKindSpecs {
		out[spec.descriptor.Kind] = spec
	}
	return out
}()

func SupportedSourceKinds() []SourceKindDescriptor {
	out := make([]SourceKindDescriptor, 0, len(sourceKindSpecs))
	for _, spec := range sourceKindSpecs {
		out = append(out, withCommonSourceFields(cloneSourceKindDescriptor(spec.descriptor)))
	}
	return out
}

func LookupSourceKind(kind SourceKind) (SourceKindDescriptor, bool) {
	spec, ok := sourceKindSpecByKind[kind]
	if !ok {
		return SourceKindDescriptor{}, false
	}
	return withCommonSourceFields(cloneSourceKindDescriptor(spec.descriptor)), true
}

func SecretFieldsForKind(kind SourceKind) []string {
	spec, ok := sourceKindSpecByKind[kind]
	if !ok || len(spec.descriptor.SecretFields) == 0 {
		return nil
	}
	return append([]string(nil), spec.descriptor.SecretFields...)
}

func IsSecretField(kind SourceKind, field string) bool {
	field = strings.TrimSpace(field)
	for _, item := range SecretFieldsForKind(kind) {
		if item == field {
			return true
		}
	}
	return false
}

func DefaultConnectors() map[SourceKind]Connector {
	out := make(map[SourceKind]Connector, len(sourceKindSpecs))
	for _, spec := range sourceKindSpecs {
		if spec.newConnector == nil {
			continue
		}
		out[spec.descriptor.Kind] = spec.newConnector()
	}
	return out
}

func NormalizeSourceConfig(kind SourceKind, config map[string]any) map[string]any {
	cfg := normalizeGenericSourceConfig(config)
	if len(cfg) == 0 {
		return nil
	}
	spec, ok := sourceKindSpecByKind[kind]
	if !ok || spec.normalizeConfig == nil {
		return cfg
	}
	return spec.normalizeConfig(cfg)
}

func NormalizeSource(source Source) (Source, error) {
	source.ID = strings.TrimSpace(source.ID)
	source.Name = strings.TrimSpace(source.Name)
	source.Locale = normalizeLocale(source.Locale)
	source.Path = strings.TrimSpace(source.Path)
	source.URLs = uniqueStrings(source.URLs)
	source.Config = NormalizeSourceConfig(source.Kind, source.Config)
	source.IncludeGlobs = uniqueStrings(source.IncludeGlobs)
	source.ExcludeGlobs = uniqueStrings(source.ExcludeGlobs)

	if source.Name == "" {
		return Source{}, fmt.Errorf("source name is required")
	}
	spec, ok := sourceKindSpecByKind[source.Kind]
	if !ok {
		return Source{}, fmt.Errorf("unsupported source kind")
	}
	if spec.validate != nil {
		if err := spec.validate(source); err != nil {
			return Source{}, err
		}
	}
	if !source.Enabled {
		source.Status = SourceStatusBlocked
	} else if source.Status == "" || source.Status == SourceStatusBlocked {
		source.Status = SourceStatusReady
	}
	source.ConnectorNote = spec.descriptor.ConnectorNote
	return source, nil
}

func cloneSourceKindDescriptor(descriptor SourceKindDescriptor) SourceKindDescriptor {
	descriptor.SecretFields = append([]string(nil), descriptor.SecretFields...)
	if len(descriptor.Fields) > 0 {
		fields := make([]SourceFieldDescriptor, len(descriptor.Fields))
		for i, field := range descriptor.Fields {
			fields[i] = field
			fields[i].Aliases = append([]string(nil), field.Aliases...)
		}
		descriptor.Fields = fields
	}
	if len(descriptor.Requirements) > 0 {
		requirements := make([]SourceRequirement, len(descriptor.Requirements))
		for i, requirement := range descriptor.Requirements {
			requirements[i] = requirement
			if len(requirement.AnyOf) > 0 {
				anyOf := make([][]string, len(requirement.AnyOf))
				for j, group := range requirement.AnyOf {
					anyOf[j] = append([]string(nil), group...)
				}
				requirements[i].AnyOf = anyOf
			}
		}
		descriptor.Requirements = requirements
	}
	return descriptor
}

func withCommonSourceFields(descriptor SourceKindDescriptor) SourceKindDescriptor {
	hasLocale := false
	for _, field := range descriptor.Fields {
		if field.Scope == SourceFieldScopeRoot && field.Key == "locale" {
			hasLocale = true
			break
		}
	}
	if !hasLocale {
		descriptor.Fields = append([]SourceFieldDescriptor{
			rootStringField("locale", "Locale", "en / zh-CN / ja", "Optional source-default locale used when documents do not carry a stronger locale signal.", false),
		}, descriptor.Fields...)
	}
	return descriptor
}

func rootStringField(key string, label string, placeholder string, description string, required bool) SourceFieldDescriptor {
	return SourceFieldDescriptor{
		ID:          key,
		Scope:       SourceFieldScopeRoot,
		Key:         key,
		Label:       label,
		Description: description,
		Type:        SourceFieldTypeString,
		Required:    required,
		Placeholder: placeholder,
	}
}

func rootStringListField(key string, label string, placeholder string, description string, required bool, rows int, aliases ...string) SourceFieldDescriptor {
	return SourceFieldDescriptor{
		ID:          key,
		Scope:       SourceFieldScopeRoot,
		Key:         key,
		Label:       label,
		Description: description,
		Type:        SourceFieldTypeStringList,
		Required:    required,
		Placeholder: placeholder,
		Rows:        rows,
		Aliases:     append([]string(nil), aliases...),
	}
}

func configStringField(key string, label string, placeholder string, description string, required bool) SourceFieldDescriptor {
	return SourceFieldDescriptor{
		ID:          key,
		Scope:       SourceFieldScopeConfig,
		Key:         key,
		Label:       label,
		Description: description,
		Type:        SourceFieldTypeString,
		Required:    required,
		Placeholder: placeholder,
	}
}

func configSecretField(key string, label string, placeholder string, description string, required bool) SourceFieldDescriptor {
	return SourceFieldDescriptor{
		ID:          key,
		Scope:       SourceFieldScopeConfig,
		Key:         key,
		Label:       label,
		Description: description,
		Type:        SourceFieldTypeString,
		Required:    required,
		Secret:      true,
		Placeholder: placeholder,
	}
}

func configStringListField(key string, label string, placeholder string, description string, required bool, rows int, aliases ...string) SourceFieldDescriptor {
	return SourceFieldDescriptor{
		ID:          key,
		Scope:       SourceFieldScopeConfig,
		Key:         key,
		Label:       label,
		Description: description,
		Type:        SourceFieldTypeStringList,
		Required:    required,
		Placeholder: placeholder,
		Rows:        rows,
		Aliases:     append([]string(nil), aliases...),
	}
}

func configBooleanField(key string, label string, description string, defaultValue bool) SourceFieldDescriptor {
	return SourceFieldDescriptor{
		ID:           key,
		Scope:        SourceFieldScopeConfig,
		Key:          key,
		Label:        label,
		Description:  description,
		Type:         SourceFieldTypeBoolean,
		DefaultValue: defaultValue,
	}
}

func normalizeGenericSourceConfig(config map[string]any) map[string]any {
	cfg := cloneConfigMap(config)
	if len(cfg) == 0 {
		return nil
	}
	for key, value := range cfg {
		switch typed := value.(type) {
		case string:
			cfg[key] = strings.TrimSpace(typed)
		case []string:
			cfg[key] = uniqueStrings(typed)
		case []any:
			lines := make([]string, 0, len(typed))
			for _, item := range typed {
				lines = append(lines, strings.TrimSpace(fmt.Sprintf("%v", item)))
			}
			cfg[key] = uniqueStrings(lines)
		}
	}
	return cfg
}

func normalizeFeishuDocsConfig(cfg map[string]any) map[string]any {
	cfg = normalizeBaseURLConfig(cfg)
	docIDs, docURLs := splitSourceRefs(append(sourceConfigStrings(cfg, "document_ids"), sourceConfigStrings(cfg, "document_urls")...))
	if len(docIDs) > 0 {
		cfg["document_ids"] = docIDs
	}
	if len(docURLs) > 0 {
		cfg["document_urls"] = docURLs
	}
	if ids := uniqueStrings(sourceConfigStrings(cfg, "wiki_node_tokens")); len(ids) > 0 {
		cfg["wiki_node_tokens"] = ids
	}
	return cfg
}

func normalizeNotionConfig(cfg map[string]any) map[string]any {
	cfg = normalizeBaseURLConfig(cfg)
	pageIDs, pageURLs := splitSourceRefs(append(sourceConfigStrings(cfg, "page_ids"), sourceConfigStrings(cfg, "page_urls")...))
	if len(pageIDs) > 0 {
		cfg["page_ids"] = pageIDs
	}
	if len(pageURLs) > 0 {
		cfg["page_urls"] = pageURLs
	}
	return cfg
}

func normalizeConfluenceConfig(cfg map[string]any) map[string]any {
	cfg = normalizeBaseURLConfig(cfg)
	pageIDs, pageURLs := splitSourceRefs(append(sourceConfigStrings(cfg, "page_ids"), sourceConfigStrings(cfg, "page_urls")...))
	if len(pageIDs) > 0 {
		cfg["page_ids"] = pageIDs
	}
	if len(pageURLs) > 0 {
		cfg["page_urls"] = pageURLs
	}
	return cfg
}

func normalizeGoogleDriveConfig(cfg map[string]any) map[string]any {
	cfg = normalizeBaseURLConfig(cfg)
	fileIDs, fileURLs := splitSourceRefs(append(sourceConfigStrings(cfg, "file_ids"), sourceConfigStrings(cfg, "file_urls")...))
	if len(fileIDs) > 0 {
		cfg["file_ids"] = fileIDs
	}
	if len(fileURLs) > 0 {
		cfg["file_urls"] = fileURLs
	}
	return cfg
}

func normalizeYuqueConfig(cfg map[string]any) map[string]any {
	cfg = normalizeBaseURLConfig(cfg)
	repoPaths, repoURLs := splitSourceRefs(append(append(sourceConfigStrings(cfg, "repo_ids"), sourceConfigStrings(cfg, "repo_paths")...), sourceConfigStrings(cfg, "repo_urls")...))
	if len(repoPaths) > 0 {
		cfg["repo_paths"] = repoPaths
	}
	if len(repoURLs) > 0 {
		cfg["repo_urls"] = repoURLs
	}
	if docs := uniqueStrings(sourceConfigStrings(cfg, "doc_urls")); len(docs) > 0 {
		cfg["doc_urls"] = docs
	}
	if slugs := uniqueStrings(sourceConfigStrings(cfg, "doc_slugs")); len(slugs) > 0 {
		cfg["doc_slugs"] = slugs
	}
	return cfg
}

func normalizeTencentDocsConfig(cfg map[string]any) map[string]any {
	cfg = normalizeBaseURLConfig(cfg)
	fileIDs, fileURLs := splitSourceRefs(append(sourceConfigStrings(cfg, "file_ids"), sourceConfigStrings(cfg, "file_urls")...))
	if len(fileIDs) > 0 {
		cfg["file_ids"] = fileIDs
	}
	if len(fileURLs) > 0 {
		cfg["file_urls"] = fileURLs
	}
	return cfg
}

func normalizeBaseURLConfig(cfg map[string]any) map[string]any {
	if baseURL := strings.TrimSpace(sourceConfigString(cfg, "base_url")); baseURL != "" {
		cfg["base_url"] = strings.TrimRight(baseURL, "/")
	}
	return cfg
}

func sourceConfigString(config map[string]any, key string) string {
	if len(config) == 0 {
		return ""
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return ""
	}
	switch typed := raw.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", typed))
	}
}

func sourceConfigStrings(config map[string]any, key string) []string {
	if len(config) == 0 {
		return nil
	}
	raw, ok := config[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return uniqueStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, strings.TrimSpace(fmt.Sprintf("%v", item)))
		}
		return uniqueStrings(out)
	case string:
		return uniqueStrings(strings.Split(typed, "\n"))
	default:
		return nil
	}
}

func hasAnySourceConfig(config map[string]any, keys ...string) bool {
	for _, key := range keys {
		if len(sourceConfigStrings(config, key)) > 0 {
			return true
		}
		if strings.TrimSpace(sourceConfigString(config, key)) != "" {
			return true
		}
	}
	return false
}

func hasAllSourceConfig(config map[string]any, keys ...string) bool {
	if len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		if len(sourceConfigStrings(config, key)) > 0 {
			continue
		}
		if strings.TrimSpace(sourceConfigString(config, key)) == "" {
			return false
		}
	}
	return true
}

func splitSourceRefs(values []string) ([]string, []string) {
	ids := make([]string, 0, len(values))
	urls := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			urls = append(urls, value)
			continue
		}
		ids = append(ids, value)
	}
	return uniqueStrings(ids), uniqueStrings(urls)
}
