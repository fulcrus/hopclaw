package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fulcrus/hopclaw/internal/nodedaemon"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	"github.com/spf13/cobra"
)

const operatorDevicesPairPath = "/operator/devices/pair"

type devicePairCreateRequest struct {
	DeviceID     string `json:"device_id"`
	Name         string `json:"name,omitempty"`
	Platform     string `json:"platform,omitempty"`
	DeviceFamily string `json:"device_family,omitempty"`
	Channel      string `json:"channel"`
}

type devicePairCreateResponse struct {
	DeviceID  string `json:"device_id"`
	Channel   string `json:"channel"`
	Code      string `json:"code"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type devicePairOutput struct {
	DeviceID        string `json:"device_id"`
	Name            string `json:"name,omitempty"`
	Platform        string `json:"platform,omitempty"`
	DeviceFamily    string `json:"device_family,omitempty"`
	Daemon          string `json:"daemon"`
	GatewayURL      string `json:"gateway_url"`
	PairingCode     string `json:"pairing_code"`
	PreferredLaunch string `json:"preferred_launch"`
	FallbackLaunch  string `json:"fallback_launch"`
}

type devicePairOptions struct {
	GatewayURL string
	AuthToken  string
	DeviceID   string
	Name       string
	Platform   string
	Family     string
}

type deviceLaunchOptions struct {
	GatewayURL  string
	PairingCode string
	DeviceID    string
	DeviceName  string
	StoreDir    string
	ListenAddr  string
	AuthToken   string
	PrintOnly   bool
}

func newDeviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "devices",
		Aliases: []string{"device"},
		Short:   "Pair and launch local device helpers",
		Long:    "Create pairing codes for desktop/browser helpers and launch them with sensible defaults.",
	}
	cmd.AddCommand(
		newDevicePairCmd(),
		newDeviceLaunchCmd(),
	)
	return cmd
}

func newDevicePairCmd() *cobra.Command {
	var opts devicePairOptions
	cmd := &cobra.Command{
		Use:   "pair <desktopd|browserd>",
		Short: "Create a helper pairing code and print the launch command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDevicePair(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.GatewayURL, "gateway-url", "", "gateway base URL override, e.g. http://127.0.0.1:16280")
	cmd.Flags().StringVar(&opts.AuthToken, "auth-token", "", "operator auth token override")
	cmd.Flags().StringVar(&opts.DeviceID, "device-id", "", "device identifier to bind to this pairing")
	cmd.Flags().StringVar(&opts.Name, "name", "", "friendly device name")
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "device platform")
	cmd.Flags().StringVar(&opts.Family, "family", "desktop", "device family")
	return cmd
}

func runDevicePair(ctx context.Context, daemon string, opts devicePairOptions) error {
	daemon, err := normalizeDaemonName(daemon)
	if err != nil {
		return err
	}

	client, gatewayURL, err := newGatewayClientWithOverride(opts.GatewayURL, opts.AuthToken)
	if err != nil {
		return err
	}

	req := devicePairCreateRequest{
		DeviceID:     normalize.FirstNonEmpty(opts.DeviceID, defaultLocalDeviceID()),
		Name:         normalize.FirstNonEmpty(opts.Name, defaultLocalDeviceName()),
		Platform:     normalize.FirstNonEmpty(opts.Platform, localPlatformName()),
		DeviceFamily: normalize.FirstNonEmpty(opts.Family, "desktop"),
		Channel:      daemon,
	}

	var resp devicePairCreateResponse
	if err := client.Post(ctx, operatorDevicesPairPath, req, &resp); err != nil {
		return err
	}

	output := devicePairOutput{
		DeviceID:        req.DeviceID,
		Name:            req.Name,
		Platform:        req.Platform,
		DeviceFamily:    req.DeviceFamily,
		Daemon:          daemon,
		GatewayURL:      gatewayURL,
		PairingCode:     resp.Code,
		PreferredLaunch: buildPreferredLaunchCommandWithCode(daemon, gatewayURL, resp.Code, req.DeviceID, req.Name, ""),
		FallbackLaunch:  buildFallbackLaunchCommandWithCode(daemon, gatewayURL, resp.Code, req.DeviceID, req.Name, ""),
	}

	if flagJSON {
		return printJSON(output)
	}

	fmt.Printf("Pairing created for %s\n", daemon)
	fmt.Printf("Device ID:    %s\n", output.DeviceID)
	if output.Name != "" {
		fmt.Printf("Device Name:  %s\n", output.Name)
	}
	if output.Platform != "" {
		fmt.Printf("Platform:     %s\n", output.Platform)
	}
	fmt.Printf("Pairing Code: %s\n", output.PairingCode)
	fmt.Println()
	fmt.Println("Recommended:")
	fmt.Printf("  %s\n", output.PreferredLaunch)
	fmt.Println()
	fmt.Println("Fallback:")
	fmt.Printf("  %s\n", output.FallbackLaunch)
	fmt.Println()
	fmt.Println("The pairing code is single-use and tied to this exact device ID.")
	return nil
}

func newDeviceLaunchCmd() *cobra.Command {
	var opts deviceLaunchOptions
	cmd := &cobra.Command{
		Use:   "launch <desktopd|browserd>",
		Short: "Launch a local helper with pairing defaults",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeviceLaunch(cmd.Context(), args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.GatewayURL, "gateway-url", "", "gateway base URL, e.g. http://127.0.0.1:16280")
	cmd.Flags().StringVar(&opts.PairingCode, "pairing-code", "", "single-use pairing code from the operator")
	cmd.Flags().StringVar(&opts.DeviceID, "device-id", "", "device identifier to claim with")
	cmd.Flags().StringVar(&opts.DeviceName, "device-name", "", "friendly device name")
	cmd.Flags().StringVar(&opts.StoreDir, "store-dir", "", "store local helper credentials under this directory")
	cmd.Flags().StringVar(&opts.ListenAddr, "listen", "", "local helper listen address override")
	cmd.Flags().StringVar(&opts.AuthToken, "auth-token", "", "local helper auth token")
	cmd.Flags().BoolVar(&opts.PrintOnly, "print", false, "print the resolved helper command instead of executing it")
	return cmd
}

func runDeviceLaunch(ctx context.Context, daemon string, opts deviceLaunchOptions) error {
	daemon, err := normalizeDaemonName(daemon)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.PairingCode) == "" {
		return fmt.Errorf("pairing-code is required")
	}

	daemonPath, err := resolveDaemonExecutable(daemon)
	if err != nil {
		return err
	}

	args := buildDaemonExecArgs(daemon, deviceLaunchOptions{
		GatewayURL:  normalize.FirstNonEmpty(opts.GatewayURL, "http://"+resolveGatewayAddr()),
		PairingCode: strings.TrimSpace(opts.PairingCode),
		DeviceID:    normalize.FirstNonEmpty(opts.DeviceID, defaultLocalDeviceID()),
		DeviceName:  normalize.FirstNonEmpty(opts.DeviceName, defaultLocalDeviceName()),
		StoreDir:    normalize.FirstNonEmpty(opts.StoreDir, nodedaemon.DefaultStoreDir(daemon)),
		ListenAddr:  strings.TrimSpace(opts.ListenAddr),
		AuthToken:   strings.TrimSpace(opts.AuthToken),
	})

	if flagJSON {
		return printJSON(map[string]any{
			"daemon":  daemon,
			"path":    daemonPath,
			"command": append([]string{daemonPath}, args...),
		})
	}
	if opts.PrintOnly {
		fmt.Println(shellJoin(append([]string{daemonPath}, args...)))
		return nil
	}

	proc := exec.CommandContext(ctx, daemonPath, args...)
	proc.Stdin = os.Stdin
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	return proc.Run()
}

func newGatewayClientWithOverride(gatewayURL, authToken string) (*GatewayClient, string, error) {
	return newGatewayClientWithOptions(gatewayURL, authToken, false)
}

func newGatewayClientWithOptions(gatewayURL, authToken string, insecure bool) (*GatewayClient, string, error) {
	client, err := newConfiguredGatewayClient()
	if err != nil {
		return nil, "", err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	if baseURL == "" {
		baseURL = client.BaseURL
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	client.BaseURL = baseURL
	if strings.TrimSpace(authToken) != "" {
		client.AuthToken = strings.TrimSpace(authToken)
	}
	if insecure {
		baseTransport := http.DefaultTransport
		if existing, ok := client.HTTP.Transport.(*http.Transport); ok && existing != nil {
			baseTransport = existing
		}
		if transport, ok := baseTransport.(*http.Transport); ok && transport != nil {
			cloned := transport.Clone()
			if cloned.TLSClientConfig == nil {
				cloned.TLSClientConfig = &tls.Config{}
			}
			cloned.TLSClientConfig.InsecureSkipVerify = true
			client.HTTP.Transport = cloned
		} else {
			client.HTTP.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}
		}
	}
	return client, baseURL, nil
}

func normalizeDaemonName(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "desktop", "desktopd", "hopclaw-desktopd":
		return "desktopd", nil
	case "browser", "browserd", "hopclaw-browserd":
		return "browserd", nil
	default:
		return "", fmt.Errorf("unsupported daemon %q (want desktopd or browserd)", value)
	}
}

func defaultLocalDeviceName() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "hopclaw-device"
	}
	return strings.TrimSpace(host)
}

func defaultLocalDeviceID() string {
	host := strings.ToLower(strings.ReplaceAll(defaultLocalDeviceName(), " ", "-"))
	return strings.ToLower(localPlatformName()) + "-" + host
}

func localPlatformName() string {
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

func buildPreferredLaunchCommand(daemon, gatewayURL, deviceID, deviceName, storeDir string) string {
	parts := []string{
		"hopclaw", "devices", "launch", daemon,
		"--gateway-url", shellQuote(gatewayURL),
		"--pairing-code", shellQuote("<PAIRING_CODE>"),
		"--device-id", shellQuote(deviceID),
	}
	if deviceName != "" {
		parts = append(parts, "--device-name", shellQuote(deviceName))
	}
	if strings.TrimSpace(storeDir) != "" {
		parts = append(parts, "--store-dir", shellQuote(storeDir))
	}
	return strings.Join(parts, " ")
}

func buildPreferredLaunchCommandWithCode(daemon, gatewayURL, pairingCode, deviceID, deviceName, storeDir string) string {
	return strings.Replace(buildPreferredLaunchCommand(daemon, gatewayURL, deviceID, deviceName, storeDir), shellQuote("<PAIRING_CODE>"), shellQuote(pairingCode), 1)
}

func buildFallbackLaunchCommand(daemon, gatewayURL, deviceID, deviceName, storeDir string) string {
	name := "hopclaw-" + daemon
	parts := []string{
		name,
		"--gateway-url", shellQuote(gatewayURL),
		"--pairing-code", shellQuote("<PAIRING_CODE>"),
		"--device-id", shellQuote(deviceID),
	}
	if deviceName != "" {
		parts = append(parts, "--device-name", shellQuote(deviceName))
	}
	if strings.TrimSpace(storeDir) != "" {
		parts = append(parts, "--store-dir", shellQuote(storeDir))
	}
	return strings.Join(parts, " ")
}

func buildFallbackLaunchCommandWithCode(daemon, gatewayURL, pairingCode, deviceID, deviceName, storeDir string) string {
	return strings.Replace(buildFallbackLaunchCommand(daemon, gatewayURL, deviceID, deviceName, storeDir), shellQuote("<PAIRING_CODE>"), shellQuote(pairingCode), 1)
}

func resolveDaemonExecutable(daemon string) (string, error) {
	name := "hopclaw-" + daemon
	candidates := []string{name}
	if runtime.GOOS == "windows" {
		candidates = append(candidates, name+".exe")
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}

	self, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(self)
		for _, candidate := range candidates {
			path := filepath.Join(dir, candidate)
			if _, statErr := os.Stat(path); statErr == nil {
				return path, nil
			}
		}
	}

	installHint := fmt.Sprintf("go install github.com/fulcrus/hopclaw/cmd/%s@latest", name)
	return "", fmt.Errorf("%s is not installed or not on PATH; run %q", name, installHint)
}

func buildDaemonExecArgs(daemon string, opts deviceLaunchOptions) []string {
	args := []string{
		"--gateway-url", strings.TrimSpace(opts.GatewayURL),
		"--pairing-code", strings.TrimSpace(opts.PairingCode),
		"--device-id", strings.TrimSpace(opts.DeviceID),
	}
	if strings.TrimSpace(opts.DeviceName) != "" {
		args = append(args, "--device-name", strings.TrimSpace(opts.DeviceName))
	}
	if strings.TrimSpace(opts.StoreDir) != "" {
		args = append(args, "--store-dir", strings.TrimSpace(opts.StoreDir))
	}
	if strings.TrimSpace(opts.ListenAddr) != "" {
		args = append(args, "--listen", strings.TrimSpace(opts.ListenAddr))
	}
	if strings.TrimSpace(opts.AuthToken) != "" {
		args = append(args, "--auth-token", strings.TrimSpace(opts.AuthToken))
	}
	return args
}

func shellJoin(parts []string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, shellQuote(part))
	}
	return strings.Join(out, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
