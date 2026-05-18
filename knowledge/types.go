package knowledge

import "time"

type SourceKind string

const (
	SourceKindLocalDir    SourceKind = "local_dir"
	SourceKindGitRepo     SourceKind = "git_repo"
	SourceKindWebURLs     SourceKind = "web_urls"
	SourceKindFeishuDocs  SourceKind = "feishu_docs"
	SourceKindNotion      SourceKind = "notion"
	SourceKindConfluence  SourceKind = "confluence"
	SourceKindGoogleDrive SourceKind = "google_drive"
	SourceKindYuque       SourceKind = "yuque"
	SourceKindTencentDocs SourceKind = "tencent_docs"
)

type SourceStatus string

const (
	SourceStatusReady    SourceStatus = "ready"
	SourceStatusSyncing  SourceStatus = "syncing"
	SourceStatusDegraded SourceStatus = "degraded"
	SourceStatusBlocked  SourceStatus = "blocked"
)

type Source struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Kind          SourceKind     `json:"kind"`
	Enabled       bool           `json:"enabled"`
	Locale        string         `json:"locale,omitempty"`
	Path          string         `json:"path,omitempty"`
	URLs          []string       `json:"urls,omitempty"`
	Config        map[string]any `json:"config,omitempty"`
	IncludeGlobs  []string       `json:"include_globs,omitempty"`
	ExcludeGlobs  []string       `json:"exclude_globs,omitempty"`
	Status        SourceStatus   `json:"status,omitempty"`
	LastSyncAt    time.Time      `json:"last_sync_at,omitempty"`
	LastError     string         `json:"last_error,omitempty"`
	SyncCursor    string         `json:"sync_cursor,omitempty"`
	Stats         SourceStats    `json:"stats,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	ConnectorNote string         `json:"connector_note,omitempty"`
}

type SourceStats struct {
	Documents int   `json:"documents"`
	Chunks    int   `json:"chunks"`
	Bytes     int64 `json:"bytes"`
}

type DocumentKind string

const (
	DocumentKindFile      DocumentKind = "file"
	DocumentKindWebPage   DocumentKind = "web_page"
	DocumentKindRemoteDoc DocumentKind = "remote_doc"
)

type DocumentMetadata struct {
	Extension string `json:"extension,omitempty"`
	MIMEType  string `json:"mime_type,omitempty"`
	ETag      string `json:"etag,omitempty"`
}

type Document struct {
	ID              string           `json:"id"`
	SourceID        string           `json:"source_id"`
	Kind            DocumentKind     `json:"kind,omitempty"`
	Title           string           `json:"title,omitempty"`
	Path            string           `json:"path,omitempty"`
	URI             string           `json:"uri,omitempty"`
	Locale          string           `json:"locale,omitempty"`
	ContentHash     string           `json:"content_hash,omitempty"`
	Bytes           int64            `json:"bytes,omitempty"`
	ChunkCount      int              `json:"chunk_count,omitempty"`
	SourceUpdatedAt time.Time        `json:"source_updated_at,omitempty"`
	SyncedAt        time.Time        `json:"synced_at,omitempty"`
	Metadata        DocumentMetadata `json:"metadata,omitempty"`
}

type DocumentSnapshot struct {
	Document Document `json:"document"`
	Content  string   `json:"content"`
}

type ChunkMetadata struct {
	TokenCount int `json:"token_count,omitempty"`
}

type Chunk struct {
	ID         string        `json:"id"`
	SourceID   string        `json:"source_id"`
	DocumentID string        `json:"document_id"`
	Ordinal    int           `json:"ordinal,omitempty"`
	Title      string        `json:"title,omitempty"`
	Path       string        `json:"path,omitempty"`
	URI        string        `json:"uri,omitempty"`
	Locale     string        `json:"locale,omitempty"`
	Content    string        `json:"content"`
	Preview    string        `json:"preview,omitempty"`
	Hash       string        `json:"hash,omitempty"`
	Bytes      int64         `json:"bytes,omitempty"`
	StartRune  int           `json:"start_rune,omitempty"`
	EndRune    int           `json:"end_rune,omitempty"`
	UpdatedAt  time.Time     `json:"updated_at,omitempty"`
	Metadata   ChunkMetadata `json:"metadata,omitempty"`
}

type ChunkVector struct {
	ChunkID     string    `json:"chunk_id"`
	SourceID    string    `json:"source_id,omitempty"`
	DocumentID  string    `json:"document_id,omitempty"`
	Locale      string    `json:"locale,omitempty"`
	Model       string    `json:"model,omitempty"`
	ContentHash string    `json:"content_hash,omitempty"`
	Vector      []float32 `json:"vector,omitempty"`
	ProjectedAt time.Time `json:"projected_at,omitempty"`
}

type SearchFilter struct {
	Query    string `json:"query,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Locale   string `json:"locale,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

type SearchResult struct {
	ChunkID      string     `json:"chunk_id"`
	SourceID     string     `json:"source_id"`
	SourceName   string     `json:"source_name,omitempty"`
	SourceKind   SourceKind `json:"source_kind,omitempty"`
	DocumentID   string     `json:"document_id,omitempty"`
	Title        string     `json:"title,omitempty"`
	Path         string     `json:"path,omitempty"`
	URI          string     `json:"uri,omitempty"`
	Locale       string     `json:"locale,omitempty"`
	Preview      string     `json:"preview,omitempty"`
	Score        float64    `json:"score,omitempty"`
	KeywordScore float64    `json:"keyword_score,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at,omitempty"`
}

type SyncResult struct {
	Source Source      `json:"source"`
	Stats  SourceStats `json:"stats"`
}
