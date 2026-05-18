package gateway

import (
	"github.com/fulcrus/hopclaw/controlplane"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
	runtimepkg "github.com/fulcrus/hopclaw/runtime"
)

type countedListResponse = apiresponse.CountedList

type countedItemsResponse = countedListResponse

type okResponse = apiresponse.OK

type errorResponse = apiresponse.Error

type nodeHeartbeatResponse = apiresponse.NodeIDOK

type namedOKResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type idOKResponse struct {
	OK bool   `json:"ok"`
	ID string `json:"id"`
}

type channelDeviceOKResponse struct {
	OK       bool   `json:"ok"`
	DeviceID string `json:"device_id"`
	Channel  string `json:"channel"`
}

type deviceTrustResponse struct {
	OK       bool   `json:"ok"`
	DeviceID string `json:"device_id"`
	Trusted  bool   `json:"trusted"`
}

type deviceRoleResponse struct {
	OK       bool   `json:"ok"`
	DeviceID string `json:"device_id"`
	Role     string `json:"role"`
}

type okResultResponse struct {
	OK     bool `json:"ok"`
	Result any  `json:"result"`
}

type channelThreadOKResponse struct {
	OK       bool   `json:"ok"`
	Channel  string `json:"channel"`
	ThreadID string `json:"thread_id"`
}

type deletedOKResponse struct {
	OK      bool   `json:"ok"`
	Deleted string `json:"deleted"`
}

type ticketOKResponse struct {
	OK     bool                     `json:"ok"`
	Ticket *runtimepkg.ApprovalView `json:"ticket"`
}

type capabilitySessionOKResponse struct {
	OK         bool   `json:"ok"`
	SessionID  string `json:"session_id"`
	Capability string `json:"capability"`
}

type pairingRevokeResponse struct {
	OK      bool   `json:"ok"`
	Channel string `json:"channel"`
	UserID  string `json:"user_id"`
}

type statusResponse struct {
	OK                bool                    `json:"ok"`
	State             string                  `json:"state,omitempty"`
	Summary           string                  `json:"summary,omitempty"`
	Version           string                  `json:"version"`
	Uptime            string                  `json:"uptime"`
	CapabilityCount   int                     `json:"capability_count"`
	ActiveRuns        int                     `json:"active_runs,omitempty"`
	QueuedRuns        int                     `json:"queued_runs,omitempty"`
	ConnectedChannels []statusChannelResponse `json:"connected_channels,omitempty"`
	Warnings          []string                `json:"warnings,omitempty"`
	UserSurface       controlplane.UserSurfaceSummary `json:"user_surface"`
	Update            any                     `json:"update,omitempty"`
}

type statusChannelResponse struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type skillConfigUpdateResponse struct {
	OK         bool           `json:"ok"`
	Name       string         `json:"name"`
	ConfigKey  string         `json:"config_key"`
	Config     map[string]any `json:"config"`
	ReloadPlan any            `json:"reload_plan"`
}
