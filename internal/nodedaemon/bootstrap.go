package nodedaemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/deviceauth"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/fulcrus/hopclaw/nodeclient"
)

type BootstrapConfig struct {
	StoreDir     string
	GatewayURL   string
	PairingCode  string
	DeviceID     string
	DeviceName   string
	Platform     string
	DeviceFamily string
	Role         deviceauth.DeviceRole
	Scopes       []string
	ExpiresAt    time.Time
}

type BootstrapResult struct {
	Store        *deviceauth.Store
	DeviceID     string
	DeviceName   string
	Platform     string
	Token        string
	WebSocketURL string
}

func Prepare(ctx context.Context, cfg BootstrapConfig) (*BootstrapResult, error) {
	if strings.TrimSpace(cfg.GatewayURL) == "" {
		return nil, nil
	}
	if cfg.Role == "" {
		cfg.Role = deviceauth.RoleNode
	}
	store := deviceauth.NewStore(cfg.StoreDir)
	if err := store.Load(); err != nil {
		return nil, fmt.Errorf("load node auth store: %w", err)
	}
	deviceID := normalize.FirstNonEmpty(cfg.DeviceID, store.PrimaryDeviceID())
	if deviceID == "" {
		deviceID = generatedDeviceID(defaultPlatform(cfg.Platform))
	}
	deviceName := normalize.FirstNonEmpty(cfg.DeviceName, hostName())
	platform := defaultPlatform(cfg.Platform)
	claimedWebSocketURL := ""

	if code := strings.TrimSpace(cfg.PairingCode); code != "" {
		claim, err := nodeclient.ClaimPairing(ctx, cfg.GatewayURL, nodeclient.PairClaimRequest{
			Code:         code,
			DeviceID:     deviceID,
			Name:         deviceName,
			Platform:     runtime.GOOS,
			DeviceFamily: cfg.DeviceFamily,
			Role:         string(cfg.Role),
			Scopes:       append([]string(nil), cfg.Scopes...),
			ExpiresAt:    formatOptionalTime(cfg.ExpiresAt),
		})
		if err != nil {
			return nil, fmt.Errorf("claim pairing code: %w", err)
		}
		if strings.TrimSpace(claim.DeviceID) != "" {
			deviceID = claim.DeviceID
		}
		claimedWebSocketURL = strings.TrimSpace(claim.WSURL)
		if err := store.SetPrimaryDeviceID(deviceID); err != nil {
			return nil, err
		}
		if err := store.RegisterDevice(&deviceauth.DeviceIdentity{
			DeviceID:     deviceID,
			Name:         deviceName,
			Platform:     runtime.GOOS,
			DeviceFamily: cfg.DeviceFamily,
			Trusted:      true,
		}); err != nil {
			return nil, err
		}
		if err := store.SetToken(&deviceauth.DeviceToken{
			Token:     claim.Token,
			DeviceID:  deviceID,
			Role:      cfg.Role,
			Scopes:    append([]string(nil), claim.Scopes...),
			IssuedAt:  time.Now().UTC(),
			ExpiresAt: parseOptionalTime(claim.ExpiresAt),
		}); err != nil {
			return nil, err
		}
	}
	token, ok := store.GetToken(deviceID, cfg.Role)
	if !ok || strings.TrimSpace(token.Token) == "" {
		return nil, fmt.Errorf("node token not found; provide --pairing-code or pair the device first")
	}
	return &BootstrapResult{
		Store:        store,
		DeviceID:     deviceID,
		DeviceName:   deviceName,
		Platform:     platform,
		Token:        token.Token,
		WebSocketURL: claimedWebSocketURL,
	}, nil
}

func DefaultStoreDir(daemon string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return daemon
	}
	return filepath.Join(home, ".hopclaw", daemon)
}

func defaultPlatform(value string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil {
		return "hopclaw-device"
	}
	return name
}

func generatedDeviceID(platform string) string {
	return strings.ToLower(strings.ReplaceAll(platform, " ", "-")) + "-" + strings.ToLower(hostName())
}

func formatOptionalTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func parseOptionalTime(raw string) time.Time {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}
