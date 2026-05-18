package gateway

type deviceTokenSummary struct {
	DeviceID     string   `json:"device_id"`
	Role         string   `json:"role"`
	Scopes       []string `json:"scopes,omitempty"`
	TokenPreview string   `json:"token_preview,omitempty"`
	IssuedAt     string   `json:"issued_at,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	UpdatedAt    string   `json:"updated_at,omitempty"`
}

type devicePairingSummary struct {
	DeviceID   string `json:"device_id"`
	Channel    string `json:"channel"`
	Code       string `json:"code"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at,omitempty"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	VerifiedAt string `json:"verified_at,omitempty"`
}

type deviceSummary struct {
	DeviceID     string                 `json:"device_id"`
	Name         string                 `json:"name,omitempty"`
	Platform     string                 `json:"platform,omitempty"`
	DeviceFamily string                 `json:"device_family,omitempty"`
	Trusted      bool                   `json:"trusted"`
	CreatedAt    string                 `json:"created_at,omitempty"`
	LastSeenAt   string                 `json:"last_seen_at,omitempty"`
	Tokens       []deviceTokenSummary   `json:"tokens,omitempty"`
	Pairings     []devicePairingSummary `json:"pairings,omitempty"`
}

type devicesListResponse struct {
	Items []deviceSummary `json:"items"`
	Count int             `json:"count"`
}

type devicePairCreateRequest struct {
	DeviceID     string `json:"device_id"`
	Name         string `json:"name,omitempty"`
	Platform     string `json:"platform,omitempty"`
	DeviceFamily string `json:"device_family,omitempty"`
	Channel      string `json:"channel"`
}

type devicePairApproveRequest struct {
	Code      string   `json:"code"`
	Role      string   `json:"role,omitempty"`
	Scopes    []string `json:"scopes,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

type devicePairClaimRequest struct {
	Code         string   `json:"code"`
	DeviceID     string   `json:"device_id,omitempty"`
	Name         string   `json:"name,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	DeviceFamily string   `json:"device_family,omitempty"`
	Role         string   `json:"role,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
}

type devicePairRejectRequest struct {
	Code     string `json:"code,omitempty"`
	Channel  string `json:"channel,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
}

type deviceTokenMutateRequest struct {
	Role      string   `json:"role"`
	Scopes    []string `json:"scopes,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
}

type devicePairCreateResponse struct {
	DeviceID   string `json:"device_id"`
	Channel    string `json:"channel"`
	Code       string `json:"code"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	VerifiedAt string `json:"verified_at"`
}

type devicePairApproveResponse struct {
	OK           bool     `json:"ok"`
	DeviceID     string   `json:"device_id"`
	Channel      string   `json:"channel"`
	Status       string   `json:"status"`
	Role         string   `json:"role"`
	Scopes       []string `json:"scopes"`
	Token        string   `json:"token"`
	TokenPreview string   `json:"token_preview"`
	ExpiresAt    string   `json:"expires_at"`
}

type devicePairClaimResponse struct {
	OK           bool     `json:"ok"`
	DeviceID     string   `json:"device_id"`
	Channel      string   `json:"channel"`
	Role         string   `json:"role"`
	Scopes       []string `json:"scopes"`
	Token        string   `json:"token"`
	TokenPreview string   `json:"token_preview"`
	ExpiresAt    string   `json:"expires_at"`
	WSURL        string   `json:"ws_url"`
}

type deviceTokenIssueResponse struct {
	OK           bool     `json:"ok"`
	DeviceID     string   `json:"device_id"`
	Role         string   `json:"role"`
	Scopes       []string `json:"scopes"`
	Token        string   `json:"token"`
	TokenPreview string   `json:"token_preview"`
	ExpiresAt    string   `json:"expires_at"`
}
