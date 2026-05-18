package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/store"
)

var (
	ErrMutationUnavailable        = errors.New("config mutation backend is not available")
	ErrEffectiveConfigUnavailable = errors.New("effective config is not available")
)

type ApprovalCallbackAuthPolicy struct {
	Mode            string        `json:"mode,omitempty"`
	HeaderName      string        `json:"header_name,omitempty"`
	Token           string        `json:"token,omitempty"`
	Secret          string        `json:"secret,omitempty"`
	SignatureHeader string        `json:"signature_header,omitempty"`
	TimestampHeader string        `json:"timestamp_header,omitempty"`
	MaxAge          time.Duration `json:"max_age,omitempty"`
}

type ApprovalCallbackAuthSummary struct {
	Protected       bool          `json:"protected"`
	Mode            string        `json:"mode,omitempty"`
	HeaderName      string        `json:"header_name,omitempty"`
	SignatureHeader string        `json:"signature_header,omitempty"`
	TimestampHeader string        `json:"timestamp_header,omitempty"`
	MaxAge          time.Duration `json:"max_age,omitempty"`
}

type ApprovalProviderDescriptor struct {
	Name          string                     `json:"name,omitempty"`
	Type          string                     `json:"type,omitempty"`
	Enabled       bool                       `json:"enabled"`
	SubmitEnabled bool                       `json:"submit_enabled,omitempty"`
	UpdateEnabled bool                       `json:"update_enabled,omitempty"`
	SyncEnabled   bool                       `json:"sync_enabled,omitempty"`
	CallbackAuth  ApprovalCallbackAuthPolicy `json:"callback_auth,omitempty"`
	Metadata      map[string]any             `json:"metadata,omitempty"`
}

type ApprovalProviderSummary struct {
	Name          string                      `json:"name"`
	Type          string                      `json:"type,omitempty"`
	Enabled       bool                        `json:"enabled"`
	Registered    bool                        `json:"registered"`
	SubmitEnabled bool                        `json:"submit_enabled"`
	UpdateEnabled bool                        `json:"update_enabled"`
	SyncEnabled   bool                        `json:"sync_enabled"`
	CallbackAuth  ApprovalCallbackAuthSummary `json:"callback_auth"`
	Metadata      map[string]any              `json:"metadata,omitempty"`
}

type GovernanceKind string

type GovernanceAdapterDescriptor struct {
	Name            string           `json:"name,omitempty"`
	Type            string           `json:"type,omitempty"`
	Enabled         bool             `json:"enabled"`
	IncludeSnapshot bool             `json:"include_snapshot"`
	Kinds           []GovernanceKind `json:"kinds,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

type GovernanceAdapterSummary struct {
	Name            string           `json:"name"`
	Type            string           `json:"type,omitempty"`
	Enabled         bool             `json:"enabled"`
	Registered      bool             `json:"registered"`
	IncludeSnapshot bool             `json:"include_snapshot"`
	Kinds           []GovernanceKind `json:"kinds,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

type AuditSinkDescriptor struct {
	Name     string         `json:"name,omitempty"`
	Type     string         `json:"type,omitempty"`
	Enabled  bool           `json:"enabled"`
	Target   string         `json:"target,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type AuditSinkSummary struct {
	Name       string         `json:"name"`
	Type       string         `json:"type,omitempty"`
	Enabled    bool           `json:"enabled"`
	Registered bool           `json:"registered"`
	Target     string         `json:"target,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ProviderProjection struct {
	Name        string                `json:"name"`
	Config      config.ProviderConfig `json:"config"`
	Enabled     *bool                 `json:"enabled,omitempty"`
	Source      string                `json:"source,omitempty"`
	BasePresent bool                  `json:"base_present,omitempty"`
}

type ChannelProjection struct {
	Name        string          `json:"name"`
	Config      json.RawMessage `json:"config"`
	Enabled     *bool           `json:"enabled,omitempty"`
	Source      string          `json:"source,omitempty"`
	Recognized  bool            `json:"recognized"`
	BasePresent bool            `json:"base_present,omitempty"`
}

type SettingProjection struct {
	Key     string          `json:"key"`
	Value   json.RawMessage `json:"value"`
	Source  string          `json:"source,omitempty"`
	Domain  string          `json:"domain,omitempty"`
	Applied bool            `json:"applied"`
	Legacy  bool            `json:"legacy,omitempty"`
}

type ApprovalProviderCatalog interface {
	Describe() []ApprovalProviderSummary
}

type GovernanceAdapterCatalog interface {
	Describe() []GovernanceAdapterSummary
}

type AuditSinkCatalog interface {
	Describe() []AuditSinkSummary
}

type EffectiveConfigProvider interface {
	Current() config.Config
	RuntimeCurrent() config.Config
	Snapshot() *EffectiveConfigSnapshot
	Providers() []ProviderProjection
	Channels() []ChannelProjection
	Settings() []SettingProjection
	Version() string
	DiffSince(version string) (config.ChangeSet, bool)
}

type ConfigMutator interface {
	SetProvider(EffectiveConfigProvider)
	PutSection(ctx context.Context, section string, sectionValue any) error
	UpsertProvider(ctx context.Context, row store.ProviderConfigRow) error
	DeleteProvider(ctx context.Context, name string, basePresent bool) error
	UpsertChannel(ctx context.Context, row store.ChannelConfigRow) error
	DeleteChannel(ctx context.Context, name string, basePresent bool) error
}
