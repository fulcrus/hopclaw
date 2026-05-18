package config

type SetupChannelFieldType string
type SupportLevel string

const (
	SetupChannelFieldString     SetupChannelFieldType = "string"
	SetupChannelFieldStringList SetupChannelFieldType = "string_list"
	SetupChannelFieldBool       SetupChannelFieldType = "bool"

	SupportLevelCore         SupportLevel = "core"
	SupportLevelSupported    SupportLevel = "supported"
	SupportLevelExperimental SupportLevel = "experimental"
)

type SetupChannelField struct {
	ID           string                `json:"id"`
	ConfigKey    string                `json:"config_key"`
	Label        string                `json:"label"`
	Description  string                `json:"description,omitempty"`
	Required     bool                  `json:"required"`
	Secret       bool                  `json:"secret,omitempty"`
	DefaultValue string                `json:"default_value,omitempty"`
	Placeholder  string                `json:"placeholder,omitempty"`
	Type         SetupChannelFieldType `json:"type,omitempty"`
}

type ChannelProfile struct {
	ID                  string              `json:"id"`
	DisplayName         string              `json:"display_name"`
	Description         string              `json:"description,omitempty"`
	Implemented         bool                `json:"implemented"`
	SupportLevel        SupportLevel        `json:"support_level"`
	SetupSupported      bool                `json:"setup_supported"`
	OnboardingSupported bool                `json:"onboarding_supported"`
	Fields              []SetupChannelField `json:"fields,omitempty"`
	OperatorFields      []SetupChannelField `json:"operator_fields,omitempty"`
}
