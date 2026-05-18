package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/model"
)

func checkGatewayPort() checkResult {
	addr := resolveGatewayAddr()
	listener, err := net.Listen("tcp", addr)
	if err == nil {
		_ = listener.Close()
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway port",
			Status:   "ok",
			Detail:   fmt.Sprintf("%s is available", addr),
		}
	}

	if isAddrInUseError(err) {
		if gatewayVisibleOnAddr(addr) {
			return checkResult{
				Category: "connectivity",
				Name:     "Gateway port",
				Status:   "ok",
				Detail:   fmt.Sprintf("%s is already serving the HopClaw gateway", addr),
			}
		}
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway port",
			Status:   "fail",
			Detail:   fmt.Sprintf("%s is already in use by another process", addr),
			Fix:      "stop the conflicting process or change server.address in config",
		}
	}

	return checkResult{
		Category: "connectivity",
		Name:     "Gateway port",
		Status:   "warn",
		Detail:   fmt.Sprintf("cannot probe %s: %v", addr, err),
	}
}

func checkGateway() checkResult {
	addr := resolveGatewayAddr()
	client, err := NewGatewayClient()
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot create gateway client: %v", err),
		}
	}
	client.HTTP.Timeout = doctorConnectTimeout
	return checkGatewayWithClient(context.Background(), client, addr)
}

func checkGatewayWithClient(parent context.Context, client *GatewayClient, addr string) checkResult {
	ctx, cancel := context.WithTimeout(parent, doctorConnectTimeout)
	defer cancel()

	body, statusCode, err := fetchOperatorStatus(ctx, client)
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway",
			Status:   "warn",
			Detail:   fmt.Sprintf("not running at %s", addr),
			Fix:      "start with 'hopclaw' or 'hopclaw daemon start'",
		}
	}

	if statusCode >= 400 {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway",
			Status:   "warn",
			Detail:   fmt.Sprintf("reachable at %s but operator status failed: %s", addr, gatewayHTTPError(statusCode, body)),
			Fix:      "verify operator auth configuration and local token",
		}
	}

	return checkResult{
		Category: "connectivity",
		Name:     "Gateway",
		Status:   "ok",
		Detail:   fmt.Sprintf("running at %s (HTTP %d)", addr, statusCode),
	}
}

func checkGatewayHealth() checkResult {
	addr := resolveGatewayAddr()
	client, err := NewGatewayClient()
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot create gateway client: %v", err),
		}
	}
	client.HTTP.Timeout = doctorConnectTimeout
	return checkGatewayHealthWithClient(context.Background(), client, addr)
}

func checkGatewayHealthWithClient(parent context.Context, client *GatewayClient, addr string) checkResult {
	ctx, cancel := context.WithTimeout(parent, doctorConnectTimeout)
	defer cancel()

	body, statusCode, err := fetchGatewayHealth(ctx, client)
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "warn",
			Detail:   "gateway not reachable; skipping health check",
		}
	}
	if statusCode >= 400 {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "fail",
			Detail:   fmt.Sprintf("gateway at %s failed public health check: %s", addr, gatewayHTTPError(statusCode, body)),
			Fix:      "inspect gateway startup logs and '/healthz' output",
		}
	}

	status, err := decodeGatewayHealth(body)
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot parse health response: %v", err),
		}
	}

	switch strings.ToLower(strings.TrimSpace(gatewayHealthLabel(status))) {
	case "ready":
		detail := "healthy"
		if summary := strings.TrimSpace(status.Summary); summary != "" && !strings.EqualFold(summary, "ready") {
			detail = summary
		}
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "ok",
			Detail:   detail,
		}
	case "degraded":
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "warn",
			Detail:   gatewayHealthSummary(status),
			Fix:      "inspect 'hopclaw status' or '/operator/controlplane/status' for actionable warnings",
		}
	default:
		return checkResult{
			Category: "connectivity",
			Name:     "Gateway health",
			Status:   "fail",
			Detail:   gatewayHealthSummary(status),
		}
	}
}

func checkChannelHealth() checkResult {
	client, err := NewGatewayClient()
	if err == nil {
		client.HTTP.Timeout = doctorConnectTimeout
		ctx, cancel := context.WithTimeout(context.Background(), doctorConnectTimeout)
		defer cancel()

		var response channelHealthListResponse
		if err := client.Get(ctx, channelsHealthPath, &response); err == nil {
			if len(response.Items) == 0 {
				return checkResult{
					Category: "connectivity",
					Name:     "Channel health",
					Status:   "ok",
					Detail:   "no active channels reported by gateway",
				}
			}

			healthy := 0
			var degraded []string
			for _, item := range response.Items {
				state := strings.TrimSpace(strings.ToLower(item.State))
				switch state {
				case "", "ready", "healthy", "connected", "running":
					healthy++
				default:
					degraded = append(degraded, item.Name)
				}
			}
			if len(degraded) == 0 {
				return checkResult{
					Category: "connectivity",
					Name:     "Channel health",
					Status:   "ok",
					Detail:   fmt.Sprintf("%d channel(s) healthy", healthy),
				}
			}
			sort.Strings(degraded)
			status := "warn"
			if healthy == 0 {
				status = "fail"
			}
			return checkResult{
				Category: "connectivity",
				Name:     "Channel health",
				Status:   status,
				Detail:   fmt.Sprintf("%d healthy, %d degraded: %s", healthy, len(degraded), strings.Join(degraded, ", ")),
				Fix:      "inspect 'hopclaw channels status' or restart the affected channel integration",
			}
		}
	}

	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel health",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel health",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	channels := detectConfiguredChannels(cfg)
	if len(channels) == 0 {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel health",
			Status:   "ok",
			Detail:   "no channels configured",
		}
	}

	return checkResult{
		Category: "connectivity",
		Name:     "Channel health",
		Status:   "ok",
		Detail:   fmt.Sprintf("%d channel(s) configured: %s", len(channels), strings.Join(channels, ", ")),
	}
}

// detectConfiguredChannels returns the names of channels that have been
// explicitly enabled (Enabled != nil && *Enabled == true) in the config.
func detectConfiguredChannels(cfg config.Config) []string {
	var channels []string
	ch := cfg.Channels
	v := reflect.ValueOf(ch)
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := v.Field(i)
		name := strings.ToLower(t.Field(i).Name)

		// Each channel config struct has an Enabled *bool field.
		enabledField := field.FieldByName("Enabled")
		if !enabledField.IsValid() || enabledField.IsNil() {
			continue
		}
		if enabledField.Elem().Bool() {
			channels = append(channels, name)
		}
	}

	return channels
}

func checkProviderConnectivity() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "connectivity",
			Name:     "Provider API",
			Status:   "warn",
			Detail:   "no config file; skipped",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Provider API",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	providers := doctorConfiguredProviders(cfg)
	if len(providers) == 0 {
		return checkResult{
			Category: "connectivity",
			Name:     "Provider API",
			Status:   "warn",
			Detail:   "no providers configured",
			Fix:      "configure at least one model provider in config or via 'hopclaw models add'",
		}
	}

	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)

	var reachable []string
	var skipped []string
	var failed []string
	for _, name := range names {
		entry := providers[name]
		baseURL := strings.TrimSpace(entry.BaseURL)
		if baseURL == "" {
			skipped = append(skipped, name+" (no base_url)")
			continue
		}

		itemCtx, itemCancel := context.WithTimeout(context.Background(), doctorConnectTimeout)
		statusCode, err := probeProviderBaseURL(itemCtx, baseURL)
		itemCancel()
		switch {
		case err != nil:
			failed = append(failed, fmt.Sprintf("%s (%v)", name, err))
		case statusCode >= http.StatusInternalServerError:
			failed = append(failed, fmt.Sprintf("%s (HTTP %d)", name, statusCode))
		default:
			reachable = append(reachable, fmt.Sprintf("%s (HTTP %d)", name, statusCode))
		}
	}

	sort.Strings(reachable)
	sort.Strings(skipped)
	sort.Strings(failed)
	if len(failed) == 0 {
		detail := fmt.Sprintf("%d provider(s) reachable", len(reachable))
		if len(skipped) > 0 {
			detail += fmt.Sprintf(", %d skipped: %s", len(skipped), strings.Join(skipped, ", "))
		}
		return checkResult{
			Category: "connectivity",
			Name:     "Provider API",
			Status:   "ok",
			Detail:   detail,
		}
	}

	status := "warn"
	if len(reachable) == 0 {
		status = "fail"
	}
	detail := fmt.Sprintf("%d reachable, %d failed", len(reachable), len(failed))
	if len(failed) > 0 {
		detail += ": " + strings.Join(failed, ", ")
	}
	if len(skipped) > 0 {
		detail += "; skipped: " + strings.Join(skipped, ", ")
	}
	return checkResult{
		Category: "connectivity",
		Name:     "Provider API",
		Status:   status,
		Detail:   detail,
		Fix:      "verify model provider base_url reachability and credentials",
	}
}

func checkChannelWebhooks() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel webhooks",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	cfg, err := config.Load(p)
	if err != nil {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel webhooks",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}

	var names []string
	var invalid []string
	for name, instance := range cfg.Channels.Webhook.Instances {
		names = append(names, "webhook:"+name)
		if err := validateWebhookURL(instance.CallbackURL); err != nil {
			invalid = append(invalid, "webhook:"+name)
		}
	}
	if strings.TrimSpace(cfg.Channels.GoogleChat.WebhookURL) != "" {
		names = append(names, "googlechat")
		if err := validateWebhookURL(cfg.Channels.GoogleChat.WebhookURL); err != nil {
			invalid = append(invalid, "googlechat")
		}
	}
	if strings.TrimSpace(cfg.Channels.SynologyChat.WebhookURL) != "" {
		names = append(names, "synology_chat")
		if err := validateWebhookURL(cfg.Channels.SynologyChat.WebhookURL); err != nil {
			invalid = append(invalid, "synology_chat")
		}
	}

	if len(names) == 0 {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel webhooks",
			Status:   "ok",
			Detail:   "no webhook-based channel endpoints configured",
		}
	}
	sort.Strings(names)
	sort.Strings(invalid)
	if len(invalid) == 0 {
		return checkResult{
			Category: "connectivity",
			Name:     "Channel webhooks",
			Status:   "ok",
			Detail:   fmt.Sprintf("%d webhook endpoint(s) valid", len(names)),
		}
	}
	status := "warn"
	if len(invalid) == len(names) {
		status = "fail"
	}
	return checkResult{
		Category: "connectivity",
		Name:     "Channel webhooks",
		Status:   status,
		Detail:   fmt.Sprintf("invalid webhook URL(s): %s", strings.Join(invalid, ", ")),
		Fix:      "set valid http(s) callback_url/webhook_url values for the affected channels",
	}
}

func validateWebhookURL(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fmt.Errorf("empty webhook url")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported webhook scheme %q", parsed.Scheme)
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("missing webhook host")
	}
	return nil
}

func doctorConfiguredProviders(cfg config.Config) map[string]model.ProviderEntry {
	providers := make(map[string]model.ProviderEntry)
	if entry, ok := config.OpenAICompatProviderEntry(cfg.Models.OpenAICompat); ok {
		providers["default"] = entry
	}
	for name, providerCfg := range cfg.Models.Providers {
		providers[name] = config.ProviderEntryFromConfig(name, providerCfg)
	}
	return model.MergeWithCatalog(providers)
}

func probeProviderBaseURL(ctx context.Context, baseURL string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, strings.TrimSpace(baseURL), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "HopClaw-Doctor")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}

func gatewayVisibleOnAddr(addr string) bool {
	client, err := NewGatewayClient()
	if err != nil {
		return false
	}
	client.BaseURL = "http://" + addr
	client.HTTP.Timeout = doctorConnectTimeout

	ctx, cancel := context.WithTimeout(context.Background(), doctorConnectTimeout)
	defer cancel()

	body, statusCode, err := fetchOperatorStatus(ctx, client)
	if err != nil {
		return false
	}
	if statusCode == http.StatusOK {
		_, decodeErr := decodeOperatorStatus(body)
		return decodeErr == nil
	}
	return statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden
}

func isAddrInUseError(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		err = opErr.Err
	}
	return strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
