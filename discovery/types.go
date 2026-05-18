package discovery

import (
	"errors"
	"time"
)

// ---------------------------------------------------------------------------
// Discovery methods
// ---------------------------------------------------------------------------

// Method identifies the peer discovery mechanism.
type Method string

const (
	// MethodMDNS discovers peers via multicast DNS on the local network.
	MethodMDNS Method = "mdns"

	// MethodStatic uses a manually configured list of peer addresses.
	MethodStatic Method = "static"

	// MethodTailscale discovers peers on a shared Tailscale tailnet.
	MethodTailscale Method = "tailscale"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// defaultServiceName is the mDNS service type used when Config.Service is empty.
const defaultServiceName = "_hopclaw._tcp"

// Config controls how HopClaw instances discover each other on the network.
type Config struct {
	Enabled      bool     `json:"enabled" yaml:"enabled"`
	Method       Method   `json:"method" yaml:"method"`                   // "mdns", "static", "tailscale"
	Service      string   `json:"service" yaml:"service"`                 // mDNS service name, default "_hopclaw._tcp"
	Peers        []string `json:"peers,omitempty" yaml:"peers,omitempty"` // static peer addresses (host:port)
	InstanceName string   `json:"instance_name" yaml:"instance_name"`     // mDNS instance name
	Port         int      `json:"port" yaml:"port"`                       // mDNS service port
	Interface    string   `json:"interface" yaml:"interface"`             // network interface to bind to
}

// ---------------------------------------------------------------------------
// Peer
// ---------------------------------------------------------------------------

// Peer represents a discovered HopClaw instance on the network.
type Peer struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Address string    `json:"address"` // host:port
	Version string    `json:"version,omitempty"`
	Status  string    `json:"status"` // "online", "offline", "unknown"
	SeenAt  time.Time `json:"seen_at"`
}

// ErrResolverStopped is returned when operations are attempted on a stopped resolver.
var ErrResolverStopped = errors.New("resolver stopped")

// Peer status constants.
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusUnknown = "unknown"
)
