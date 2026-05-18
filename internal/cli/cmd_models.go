package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/model"
	"github.com/fulcrus/hopclaw/modelrouter"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	modelsBasePath        = "/operator/models"
	modelsRouterPath      = "/operator/models/router"
	modelsStatusPath      = "/operator/status"
	modelsCompletionsPath = "/v1/chat/completions"
	modelsBenchIterations = 3
	modelsTestTimeout     = 30 * time.Second
)

var (
	modelsProgressWriter    io.Writer = os.Stderr
	modelsProgressInterval            = 120 * time.Millisecond
	modelsProgressCanRender           = defaultModelsProgressCanRender
	modelsProgressFrames              = []string{"-", "\\", "|", "/"}
)

// ---------------------------------------------------------------------------
// Response types (mirror the API JSON shapes)
// ---------------------------------------------------------------------------

// modelsTestResult captures the outcome of a test probe to a provider.
type modelsTestResult struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Status    string `json:"status"` // "ok", "fail"
	LatencyMs int64  `json:"latency_ms"`
	Tokens    int    `json:"tokens,omitempty"`
	Error     string `json:"error,omitempty"`
}

// modelsInfoResult shows provider configuration details.
type modelsInfoResult struct {
	Name         string            `json:"name"`
	API          string            `json:"api,omitempty"`
	BaseURL      string            `json:"base_url,omitempty"`
	Region       string            `json:"region,omitempty"`
	DefaultModel string            `json:"default_model,omitempty"`
	HasAPIKey    bool              `json:"has_api_key"`
	APIKeysCount int               `json:"api_keys_count,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	HeaderCount  int               `json:"header_count,omitempty"`
	ConfigSource string            `json:"config_source,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
}

// modelsBenchResult captures latency benchmark results for one provider.
type modelsBenchResult struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	MinMs    int64  `json:"min_ms"`
	AvgMs    int64  `json:"avg_ms"`
	MaxMs    int64  `json:"max_ms"`
	Failures int    `json:"failures"`
}

type modelsListRow struct {
	Provider          string `json:"provider"`
	Default           bool   `json:"default"`
	API               string `json:"api"`
	DefaultModel      string `json:"default_model"`
	AuthConfigured    bool   `json:"auth_configured"`
	ContextWindow     int    `json:"context_window"`
	MaxOutputTokens   int    `json:"max_output_tokens"`
	SupportsTools     bool   `json:"supports_tools"`
	SupportsStreaming bool   `json:"supports_streaming"`
	SupportsVision    bool   `json:"supports_vision"`
	SupportsThinking  bool   `json:"supports_thinking"`
	Source            string `json:"source"`
	ConfigSource      string `json:"config_source,omitempty"`
	ConfigScope       string `json:"config_scope,omitempty"`
}

type modelsOperatorProvider struct {
	Name             string                 `json:"name"`
	API              string                 `json:"api"`
	BaseURL          string                 `json:"base_url"`
	Region           string                 `json:"region,omitempty"`
	DefaultModel     string                 `json:"default_model"`
	HasKey           bool                   `json:"has_key"`
	APIKeysCount     int                    `json:"api_keys_count,omitempty"`
	Timeout          string                 `json:"timeout,omitempty"`
	HeaderCount      int                    `json:"header_count,omitempty"`
	Source           string                 `json:"source,omitempty"`
	Mutable          bool                   `json:"mutable"`
	ConfigScope      string                 `json:"config_scope,omitempty"`
	CapabilityMatrix model.CapabilityMatrix `json:"capability_matrix,omitempty"`
}

type modelsOperatorListResponse struct {
	Providers         []modelsOperatorProvider `json:"providers"`
	Count             int                      `json:"count"`
	DefaultProvider   string                   `json:"default_provider,omitempty"`
	AgentDefaultModel string                   `json:"agent_default_model,omitempty"`
}

type modelsOperatorRouterResponse struct {
	Profiles        []modelrouter.ProfileView `json:"profiles"`
	Count           int                       `json:"count"`
	DefaultProvider string                    `json:"default_provider,omitempty"`
}

type modelProviderState struct {
	Providers          map[string]model.ProviderEntry
	DefaultProvider    string
	AgentDefaultModel  string
	Details            map[string]modelProviderDetail
	CapabilityMatrices map[string]model.CapabilityMatrix
}

type modelProviderDetail struct {
	AuthConfigured bool
	APIKeysCount   int
	ConfigSource   string
	Timeout        string
	HeaderCount    int
	ConfigScope    string
	Mutable        bool
}

// modelsCompletionRequest is the request body for a chat completion probe.
type modelsCompletionRequest struct {
	Model    string                    `json:"model"`
	Messages []modelsCompletionMessage `json:"messages"`
}

// modelsCompletionMessage is a single message in a completion request.
type modelsCompletionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// modelsCompletionResponse is the response from a chat completion endpoint.
type modelsCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type modelsProgressIndicator struct {
	writer  io.Writer
	stop    chan struct{}
	done    chan struct{}
	enabled bool

	mu      sync.Mutex
	message string
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newModelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "models",
		Short: "Show model and capability information",
		Long:  "Display configured model status and capabilities from the running gateway.",
	}

	cmd.AddCommand(
		newModelsListCmd(),
		newModelsRouterCmd(),
		newModelsStatusCmd(),
		newModelsTestCmd(),
		newModelsValidateCmd(),
		newModelsTestChatCmd(),
		newModelsInfoCmd(),
		newModelsBenchCmd(),
		newModelsAddCmd(),
		newModelsUpdateCmd(),
		newModelsDeleteCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// models list
// ---------------------------------------------------------------------------

func newModelsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured model providers",
		Long:  "List configured model providers from the operator effective config, with local fallback when the gateway is unavailable.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsList(cmd.Context())
		},
	}
}

func runModelsList(ctx context.Context) error {
	client, _ := NewGatewayClient()
	catalog := loadCLISetupCatalog(ctx, client)
	state, err := loadModelProviderState(ctx, client)
	if err != nil {
		return err
	}
	matrices := model.CapabilityMatricesForProviders(state.Providers)
	rows := make([]modelsListRow, 0, len(matrices))
	for _, computed := range matrices {
		entry := state.Providers[computed.ProviderName]
		matrix := capabilityMatrixForState(state, computed.ProviderName, entry)
		detail := modelProviderDetailForState(state, matrix.ProviderName, entry)
		rows = append(rows, modelsListRow{
			Provider:          matrix.ProviderName,
			Default:           matrix.ProviderName == state.DefaultProvider,
			API:               string(matrix.ProviderAPI),
			DefaultModel:      matrix.Model,
			AuthConfigured:    detail.AuthConfigured,
			ContextWindow:     matrix.ContextWindow,
			MaxOutputTokens:   matrix.MaxOutputTokens,
			SupportsTools:     matrix.SupportsTools,
			SupportsStreaming: matrix.SupportsStreaming,
			SupportsVision:    matrix.SupportsVision,
			SupportsThinking:  matrix.SupportsReasoning,
			Source:            matrix.Source,
			ConfigSource:      detail.ConfigSource,
			ConfigScope:       detail.ConfigScope,
		})
	}
	sortModelRowsByCatalog(rows, catalog)
	if flagJSON {
		return printJSON(rows)
	}

	if len(rows) == 0 {
		fmt.Println("no model providers configured")
		return nil
	}

	fmt.Printf("%-16s  %-7s  %-20s  %-6s  %-5s  %-7s  %-7s  %s\n", "PROVIDER", "DEFAULT", "MODEL", "AUTH", "TOOLS", "STREAM", "VISION", "API")
	fmt.Printf("%-16s  %-7s  %-20s  %-6s  %-5s  %-7s  %-7s  %s\n", "--------", "-------", "-----", "----", "-----", "------", "------", "---")
	for _, row := range rows {
		defaultMark := ""
		if row.Default {
			defaultMark = "*"
		}
		fmt.Printf("%-16s  %-7s  %-20s  %-6v  %-5v  %-7v  %-7v  %s\n",
			truncate(row.Provider, 16),
			defaultMark,
			truncate(row.DefaultModel, 20),
			row.AuthConfigured,
			row.SupportsTools,
			row.SupportsStreaming,
			row.SupportsVision,
			truncate(row.API, 24),
		)
	}
	fmt.Printf("\nTotal: %d providers\n", len(rows))
	return nil
}

func newModelsRouterCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "router",
		Short: "Show effective router profiles",
		Long:  "Show the effective router profiles built from the operator model surface, with local fallback when the gateway is unavailable.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsRouter(cmd.Context())
		},
	}
}

func runModelsRouter(ctx context.Context) error {
	client, _ := NewGatewayClient()
	profiles, err := loadModelRouterProfiles(ctx, client)
	if err != nil {
		return err
	}
	if flagJSON {
		return printJSON(modelrouter.ProfileViewsFromProfiles(profiles))
	}
	if len(profiles) == 0 {
		fmt.Println("no router profiles available")
		return nil
	}
	fmt.Printf("%-28s  %-14s  %-8s  %-8s  %s\n", "PROFILE", "PROVIDER", "TOOLS", "VISION", "PRIORITY")
	fmt.Printf("%-28s  %-14s  %-8s  %-8s  %s\n", "-------", "--------", "-----", "------", "--------")
	for _, profile := range profiles {
		fmt.Printf("%-28s  %-14s  %-8v  %-8v  %d\n",
			truncate(profile.ID, 28),
			truncate(profile.Provider, 14),
			profile.Supports[modelrouter.CapabilityTools],
			profile.Supports[modelrouter.CapabilityVision],
			profile.Priority,
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// models status
// ---------------------------------------------------------------------------

func newModelsStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show gateway status",
		Long:  "Show gateway status including version and capability count.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsStatus(cmd.Context())
		},
	}
}

func runModelsStatus(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	return runModelsStatusWithClient(ctx, client, flagJSON)
}

func runModelsStatusWithClient(ctx context.Context, client *GatewayClient, jsonOutput bool) error {
	body, statusCode, err := fetchOperatorStatus(ctx, client)
	if err != nil {
		return err
	}
	if statusCode >= 400 {
		return gatewayHTTPError(statusCode, body)
	}

	status, err := decodeOperatorStatus(body)
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSON(status)
	}

	fmt.Printf("OK:           %v\n", status.OK)
	if status.Version != "" {
		fmt.Printf("Version:      %s\n", status.Version)
	}
	if status.Uptime != "" {
		fmt.Printf("Uptime:       %s\n", status.Uptime)
	}
	fmt.Printf("Capabilities: %d\n", status.CapabilityCount)
	return nil
}

// ---------------------------------------------------------------------------
// models test [provider]
// ---------------------------------------------------------------------------

func newModelsTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test [provider]",
		Short: "Send a test probe to a model provider",
		Long:  "Send a simple 'Hello' prompt to the specified provider (or default) and report latency and token count.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var provider string
			if len(args) > 0 {
				provider = args[0]
			}
			return runModelsTest(cmd.Context(), provider)
		},
	}
}

func runModelsTest(ctx context.Context, provider string) error {
	client, err := newGatewayClient()
	if err != nil {
		return err
	}

	resolvedProvider, modelID, err := resolveTestTarget(ctx, client, provider)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, modelsTestTimeout)
	defer cancel()

	start := time.Now()
	req := modelsCompletionRequest{
		Model: modelID,
		Messages: []modelsCompletionMessage{
			{Role: "user", Content: "Hello"},
		},
	}
	if !flagJSON {
		fmt.Printf("Testing provider: %s\n", providerOrDefault(resolvedProvider))
		fmt.Printf("Testing model:    %s\n", modelID)
	}

	progress := startModelsProgressIndicator("Probing %s", providerOrDefault(resolvedProvider))
	var resp modelsCompletionResponse
	err = client.Post(ctx, modelsCompletionsPath, req, &resp)
	progress.Stop()
	elapsed := time.Since(start)

	result := modelsTestResult{
		Provider:  providerOrDefault(resolvedProvider),
		Model:     modelID,
		LatencyMs: elapsed.Milliseconds(),
	}

	if err != nil {
		result.Status = "fail"
		result.Error = err.Error()
	} else {
		result.Status = "ok"
		result.Tokens = resp.Usage.TotalTokens
	}

	if flagJSON {
		return printJSON(result)
	}

	fmt.Printf("Provider: %s\n", providerOrDefault(resolvedProvider))
	fmt.Printf("Model:    %s\n", modelID)
	fmt.Printf("Status:   %s\n", result.Status)
	fmt.Printf("Latency:  %d ms\n", result.LatencyMs)
	if result.Tokens > 0 {
		fmt.Printf("Tokens:   %d\n", result.Tokens)
	}
	if result.Error != "" {
		fmt.Printf("Error:    %s\n", result.Error)
	}

	if result.Status == "fail" {
		return silentExitError(1)
	}
	return nil
}

// ---------------------------------------------------------------------------
// models info <name>
// ---------------------------------------------------------------------------

func newModelsInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <provider>",
		Short: "Show detailed provider configuration",
		Long:  "Display the configuration details for a named model provider.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runModelsInfo(cmd.Context(), args[0])
		},
	}
}

func runModelsInfo(ctx context.Context, name string) error {
	client, _ := NewGatewayClient()
	state, err := loadModelProviderState(ctx, client)
	if err != nil {
		return err
	}
	resolvedName := strings.TrimSpace(name)
	if resolvedName == "default" {
		if actual, resolveErr := resolveRequestedProvider(resolvedName, state); resolveErr == nil {
			resolvedName = actual
		}
	}
	entry, ok := state.Providers[resolvedName]
	if !ok {
		return fmt.Errorf("provider %q not found", name)
	}
	detail := modelProviderDetailForState(state, resolvedName, entry)

	result := modelsInfoResult{
		Name:         resolvedName,
		API:          string(effectiveModelProviderAPI(entry)),
		BaseURL:      entry.BaseURL,
		Region:       entry.Region,
		DefaultModel: entry.DefaultModel,
		HasAPIKey:    detail.AuthConfigured,
		APIKeysCount: detail.APIKeysCount,
		Timeout:      detail.Timeout,
		HeaderCount:  detail.HeaderCount,
		ConfigSource: detail.ConfigSource,
		Headers:      cloneModelHeaders(entry.Headers),
	}

	if flagJSON {
		return printJSON(map[string]any{
			"provider":          result,
			"is_default":        resolvedName == state.DefaultProvider,
			"capability_matrix": capabilityMatrixForState(state, resolvedName, entry),
		})
	}
	printModelsInfoResult(result)
	if resolvedName == state.DefaultProvider {
		fmt.Println("Default:       true")
	}
	return nil
}

func printModelsInfoResult(r modelsInfoResult) {
	fmt.Printf("Provider:      %s\n", r.Name)
	if r.API != "" {
		fmt.Printf("API:           %s\n", r.API)
	}
	if r.BaseURL != "" {
		fmt.Printf("Base URL:      %s\n", r.BaseURL)
	}
	if r.Region != "" {
		fmt.Printf("Region:        %s\n", r.Region)
	}
	if r.DefaultModel != "" {
		fmt.Printf("Default Model: %s\n", r.DefaultModel)
	}
	fmt.Printf("API Key:       %v\n", r.HasAPIKey)
	if r.APIKeysCount > 0 {
		fmt.Printf("Key Pool:      %d configured\n", r.APIKeysCount)
	}
	if r.Timeout != "" && r.Timeout != "0s" {
		fmt.Printf("Timeout:       %s\n", r.Timeout)
	}
	if r.HeaderCount > 0 {
		fmt.Printf("Headers:       %d custom\n", r.HeaderCount)
	}
	if r.ConfigSource != "" {
		fmt.Printf("Source:        %s\n", r.ConfigSource)
	}
}

// ---------------------------------------------------------------------------
// models bench
// ---------------------------------------------------------------------------

func newModelsBenchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bench",
		Short: "Benchmark model provider latency",
		Long: fmt.Sprintf("Send %d requests to each configured provider and report min/avg/max latency.",
			modelsBenchIterations),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runModelsBench(cmd.Context())
		},
	}
}

func runModelsBench(ctx context.Context) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	state, err := loadModelProviderState(ctx, client)
	if err != nil {
		return err
	}

	// Collect providers to benchmark.
	type providerModel struct {
		name  string
		model string
	}
	var providers []providerModel
	names := orderedModelProviderNames(state.Providers, catalog)
	for _, name := range names {
		entry := state.Providers[name]
		modelID := entry.DefaultModel
		if modelID == "" {
			modelID = state.AgentDefaultModel
		}
		if modelID == "" {
			modelID = "unknown"
		}
		providers = append(providers, providerModel{name: name, model: modelID})
	}

	if len(providers) == 0 {
		return fmt.Errorf("no providers configured to benchmark")
	}

	var results []modelsBenchResult
	progress := startModelsProgressIndicator("Benchmarking %d provider(s)", len(providers))

	for index, prov := range providers {
		progress.Update("Benchmarking %s (%d/%d)", prov.name, index+1, len(providers))
		result := benchProvider(ctx, client, prov.name, prov.model)
		results = append(results, result)
	}
	progress.Stop()

	if flagJSON {
		return printJSON(results)
	}

	fmt.Printf("%-16s  %-24s  %8s  %8s  %8s  %s\n",
		"PROVIDER", "MODEL", "MIN", "AVG", "MAX", "FAIL")
	fmt.Printf("%-16s  %-24s  %8s  %8s  %8s  %s\n",
		"--------", "-----", "---", "---", "---", "----")

	for _, r := range results {
		fmt.Printf("%-16s  %-24s  %6d ms  %6d ms  %6d ms  %d/%d\n",
			r.Provider,
			truncate(r.Model, 24),
			r.MinMs, r.AvgMs, r.MaxMs,
			r.Failures, modelsBenchIterations,
		)
	}

	return nil
}

func benchProvider(ctx context.Context, client *GatewayClient, providerName, model string) modelsBenchResult {
	result := modelsBenchResult{
		Provider: providerName,
		Model:    model,
	}

	var latencies []int64

	for i := 0; i < modelsBenchIterations; i++ {
		iterCtx, cancel := context.WithTimeout(ctx, modelsTestTimeout)
		start := time.Now()

		req := modelsCompletionRequest{
			Model: model,
			Messages: []modelsCompletionMessage{
				{Role: "user", Content: "Hello"},
			},
		}

		var resp modelsCompletionResponse
		err := client.Post(iterCtx, modelsCompletionsPath, req, &resp)
		elapsed := time.Since(start).Milliseconds()
		cancel()

		if err != nil {
			result.Failures++
			continue
		}

		latencies = append(latencies, elapsed)
	}

	if len(latencies) > 0 {
		var total int64
		result.MinMs = latencies[0]
		result.MaxMs = latencies[0]
		for _, l := range latencies {
			total += l
			if l < result.MinMs {
				result.MinMs = l
			}
			if l > result.MaxMs {
				result.MaxMs = l
			}
		}
		result.AvgMs = total / int64(len(latencies))
	}

	return result
}

func defaultModelsProgressCanRender() bool {
	if flagJSON {
		return false
	}
	_, tty := writerFile(modelsProgressWriter)
	return tty
}

func startModelsProgressIndicator(format string, args ...any) *modelsProgressIndicator {
	indicator := &modelsProgressIndicator{
		writer:  modelsProgressWriter,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		enabled: modelsProgressCanRender(),
		message: fmt.Sprintf(format, args...),
	}
	if !indicator.enabled {
		close(indicator.done)
		return indicator
	}
	go indicator.run()
	return indicator
}

func (p *modelsProgressIndicator) Update(format string, args ...any) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.message = fmt.Sprintf(format, args...)
	p.mu.Unlock()
}

func (p *modelsProgressIndicator) Stop() {
	if p == nil || !p.enabled {
		return
	}
	select {
	case <-p.done:
		return
	default:
	}
	close(p.stop)
	<-p.done
}

func (p *modelsProgressIndicator) run() {
	defer close(p.done)

	frameIndex := 0
	ticker := time.NewTicker(modelsProgressInterval)
	defer ticker.Stop()

	render := func(clear bool) {
		p.mu.Lock()
		message := p.message
		p.mu.Unlock()
		if clear {
			fmt.Fprint(p.writer, "\r\033[2K")
			return
		}
		fmt.Fprintf(p.writer, "\r\033[2K[%s] %s", modelsProgressFrames[frameIndex%len(modelsProgressFrames)], message)
		frameIndex++
	}

	render(false)
	for {
		select {
		case <-p.stop:
			render(true)
			return
		case <-ticker.C:
			render(false)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// resolveTestTarget determines which provider/model pair should be used for a
// chat-completion probe. It prefers the operator's effective config and falls
// back to local YAML when the gateway is unavailable.
func resolveTestTarget(ctx context.Context, client *GatewayClient, provider string) (string, string, error) {
	state, err := loadModelProviderState(ctx, client)
	if err != nil {
		return "", "", err
	}
	resolvedProvider, err := resolveRequestedProvider(provider, state)
	if err != nil {
		return "", "", err
	}
	entry := state.Providers[resolvedProvider]
	modelID := strings.TrimSpace(entry.DefaultModel)
	if modelID == "" {
		modelID = strings.TrimSpace(state.AgentDefaultModel)
	}
	if modelID == "" {
		return "", "", fmt.Errorf("provider %q has no default model configured", resolvedProvider)
	}
	return resolvedProvider, modelID, nil
}

func providerOrDefault(name string) string {
	if name == "" {
		return "default"
	}
	return name
}

func loadModelProviderState(ctx context.Context, client *GatewayClient) (modelProviderState, error) {
	if strings.TrimSpace(flagConfig) != "" {
		state, err := loadLocalModelProviderState()
		if err == nil {
			return state, nil
		}
		if client == nil {
			return modelProviderState{}, err
		}
	}
	var operatorErr error
	if client != nil {
		state, err := loadOperatorModelState(ctx, client)
		if err == nil {
			return state, nil
		}
		operatorErr = err
	}
	state, err := loadLocalModelProviderState()
	if err == nil {
		return state, nil
	}
	if operatorErr != nil {
		return modelProviderState{}, operatorErr
	}
	return modelProviderState{}, err
}

func loadOperatorModelState(ctx context.Context, client *GatewayClient) (modelProviderState, error) {
	if client == nil {
		return modelProviderState{}, fmt.Errorf("gateway client is not configured")
	}
	var resp modelsOperatorListResponse
	if err := client.Get(ctx, modelsBasePath, &resp); err != nil {
		return modelProviderState{}, err
	}
	state := modelProviderState{
		Providers:          make(map[string]model.ProviderEntry, len(resp.Providers)),
		DefaultProvider:    strings.TrimSpace(resp.DefaultProvider),
		AgentDefaultModel:  strings.TrimSpace(resp.AgentDefaultModel),
		Details:            make(map[string]modelProviderDetail, len(resp.Providers)),
		CapabilityMatrices: make(map[string]model.CapabilityMatrix, len(resp.Providers)),
	}
	for _, item := range resp.Providers {
		mutable := item.Mutable
		if strings.TrimSpace(item.ConfigScope) != "openai_compat" {
			mutable = true
		}
		state.Providers[item.Name] = model.ProviderEntry{
			API:          model.ProviderAPI(item.API),
			BaseURL:      item.BaseURL,
			Region:       item.Region,
			DefaultModel: item.DefaultModel,
		}
		state.Details[item.Name] = modelProviderDetail{
			AuthConfigured: item.HasKey,
			APIKeysCount:   item.APIKeysCount,
			ConfigSource:   normalizeModelConfigSource(item.Source),
			Timeout:        strings.TrimSpace(item.Timeout),
			HeaderCount:    item.HeaderCount,
			ConfigScope:    strings.TrimSpace(item.ConfigScope),
			Mutable:        mutable,
		}
		state.CapabilityMatrices[item.Name] = item.CapabilityMatrix
	}
	state.DefaultProvider = normalizeModelDefaultProvider(state.DefaultProvider, state.Providers)
	return state, nil
}

func loadLocalModelProviderState() (modelProviderState, error) {
	p := resolveConfigPath()
	if p == "" {
		return modelProviderState{}, fmt.Errorf("no config file found; run 'hopclaw setup'")
	}
	cfg, err := config.Load(p)
	if err != nil {
		return modelProviderState{}, fmt.Errorf("load config: %w", err)
	}
	return buildLocalModelProviderState(cfg)
}

func buildLocalModelProviderState(cfg config.Config) (modelProviderState, error) {
	providers, defaultProvider, err := buildCLIModelProviders(cfg.Models)
	if err != nil {
		return modelProviderState{}, err
	}
	state := modelProviderState{
		Providers:          providers,
		DefaultProvider:    normalizeModelDefaultProvider(defaultProvider, providers),
		AgentDefaultModel:  strings.TrimSpace(cfg.Agent.DefaultModel),
		Details:            make(map[string]modelProviderDetail, len(providers)),
		CapabilityMatrices: make(map[string]model.CapabilityMatrix, len(providers)),
	}
	if strings.TrimSpace(cfg.Models.OpenAICompat.BaseURL) != "" {
		state.Details["default"] = modelProviderDetail{
			AuthConfigured: strings.TrimSpace(cfg.Models.OpenAICompat.APIKey) != "",
			APIKeysCount:   0,
			ConfigSource:   "openai_compat",
			Timeout:        configuredDurationString(cfg.Models.OpenAICompat.Timeout),
			HeaderCount:    len(cfg.Models.OpenAICompat.Headers),
			ConfigScope:    "openai_compat",
			Mutable:        false,
		}
	}
	for name, item := range cfg.Models.Providers {
		state.Details[name] = modelProviderDetail{
			AuthConfigured: config.ProviderConfigHasCredentials(item),
			APIKeysCount:   len(item.APIKeys),
			ConfigSource:   normalizeModelConfigSource("yaml"),
			Timeout:        configuredDurationString(item.Timeout),
			HeaderCount:    len(item.Headers),
			ConfigScope:    "providers",
			Mutable:        true,
		}
	}
	for name, entry := range providers {
		if _, ok := state.Details[name]; ok {
			state.CapabilityMatrices[name] = model.CapabilityMatrixForProvider(name, entry)
			continue
		}
		state.Details[name] = modelProviderDetail{
			AuthConfigured: hasProviderCredentials(entry),
			APIKeysCount:   len(entry.APIKeys),
			ConfigSource:   "yaml",
			Timeout:        configuredDurationString(entry.Timeout),
			HeaderCount:    len(entry.Headers),
			ConfigScope:    "providers",
			Mutable:        true,
		}
		state.CapabilityMatrices[name] = model.CapabilityMatrixForProvider(name, entry)
	}
	return state, nil
}

func buildCLIModelProviders(cfg config.ModelsConfig) (map[string]model.ProviderEntry, string, error) {
	providers := make(map[string]model.ProviderEntry)
	if entry, ok := config.OpenAICompatProviderEntry(cfg.OpenAICompat); ok {
		providers["default"] = entry
	}
	for name, pcfg := range cfg.Providers {
		providers[name] = config.ProviderEntryFromConfig(name, pcfg)
	}
	providers = model.MergeWithCatalog(providers)
	for name, entry := range providers {
		if catalog, ok := model.CatalogLookup(name); ok && catalog.RequireBaseURL && strings.TrimSpace(entry.BaseURL) == "" {
			return nil, "", fmt.Errorf("models.providers.%s.base_url is required for this catalog provider", name)
		}
	}
	defaultProvider := strings.TrimSpace(cfg.DefaultProvider)
	if defaultProvider == "" {
		if _, ok := providers["default"]; ok {
			defaultProvider = "default"
		} else if len(providers) == 1 {
			for name := range providers {
				defaultProvider = name
			}
		}
	}
	return providers, defaultProvider, nil
}

func hasProviderCredentials(entry model.ProviderEntry) bool {
	return strings.TrimSpace(entry.APIKey) != "" ||
		len(entry.APIKeys) > 0 ||
		(strings.TrimSpace(entry.AccessKeyID) != "" && strings.TrimSpace(entry.SecretKey) != "")
}

func modelProviderDetailForState(state modelProviderState, provider string, entry model.ProviderEntry) modelProviderDetail {
	detail, ok := state.Details[provider]
	if !ok {
		detail.AuthConfigured = hasProviderCredentials(entry)
		detail.APIKeysCount = len(entry.APIKeys)
		detail.ConfigSource = "yaml"
		detail.Timeout = configuredDurationString(entry.Timeout)
		detail.HeaderCount = len(entry.Headers)
		detail.ConfigScope = "providers"
		detail.Mutable = true
	}
	if detail.Timeout == "" {
		detail.Timeout = configuredDurationString(entry.Timeout)
	}
	if detail.HeaderCount == 0 && len(entry.Headers) > 0 {
		detail.HeaderCount = len(entry.Headers)
	}
	if detail.APIKeysCount == 0 && len(entry.APIKeys) > 0 {
		detail.APIKeysCount = len(entry.APIKeys)
	}
	if detail.ConfigSource == "" {
		detail.ConfigSource = "yaml"
	}
	if detail.ConfigScope == "" {
		detail.ConfigScope = "providers"
	}
	return detail
}

func capabilityMatrixForState(state modelProviderState, provider string, entry model.ProviderEntry) model.CapabilityMatrix {
	provider = strings.TrimSpace(provider)
	if matrix, ok := state.CapabilityMatrices[provider]; ok && model.HasCapabilityMatrixContract(matrix) {
		if strings.TrimSpace(matrix.ProviderName) == "" {
			matrix.ProviderName = provider
		}
		return matrix
	}
	return model.CapabilityMatrixForProvider(provider, entry)
}

func routerProfilesForState(state modelProviderState) []modelrouter.ModelProfile {
	return model.BuildRouterProfilesWithProviderCapabilities(state.Providers, state.CapabilityMatrices, state.DefaultProvider)
}

func loadModelRouterProfiles(ctx context.Context, client *GatewayClient) ([]modelrouter.ModelProfile, error) {
	if client != nil {
		profiles, err := loadOperatorModelRouterProfiles(ctx, client)
		if err == nil {
			return profiles, nil
		}
	}
	state, err := loadModelProviderState(ctx, client)
	if err != nil {
		return nil, err
	}
	return routerProfilesForState(state), nil
}

func loadOperatorModelRouterProfiles(ctx context.Context, client *GatewayClient) ([]modelrouter.ModelProfile, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is not configured")
	}
	var resp modelsOperatorRouterResponse
	if err := client.Get(ctx, modelsRouterPath, &resp); err != nil {
		return nil, err
	}
	if len(resp.Profiles) == 0 {
		return nil, nil
	}
	profiles := make([]modelrouter.ModelProfile, len(resp.Profiles))
	for i, profile := range resp.Profiles {
		profiles[i] = profile.ModelProfile()
	}
	return profiles, nil
}

func resolveRequestedProvider(requested string, state modelProviderState) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" || requested == "default" {
		resolved := normalizeModelDefaultProvider(state.DefaultProvider, state.Providers)
		if resolved == "" {
			return "", fmt.Errorf("no default model provider configured")
		}
		return resolved, nil
	}
	if _, ok := state.Providers[requested]; !ok {
		return "", fmt.Errorf("provider %q not found", requested)
	}
	return requested, nil
}

func normalizeModelDefaultProvider(defaultProvider string, providers map[string]model.ProviderEntry) string {
	return model.ResolveDefaultProvider(providers, defaultProvider)
}

func orderedModelProviderNames(providers map[string]model.ProviderEntry, catalog cliSetupCatalog) []string {
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.SliceStable(names, func(i, j int) bool {
		leftRank, leftKnown := modelProviderSortRank(names[i], catalog)
		rightRank, rightKnown := modelProviderSortRank(names[j], catalog)
		switch {
		case leftKnown && rightKnown && leftRank != rightRank:
			return leftRank < rightRank
		case leftKnown && !rightKnown:
			return true
		case !leftKnown && rightKnown:
			return false
		default:
			return names[i] < names[j]
		}
	})
	return names
}

func sortModelRowsByCatalog(rows []modelsListRow, catalog cliSetupCatalog) {
	if len(rows) < 2 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftRank, leftKnown := modelProviderSortRank(rows[i].Provider, catalog)
		rightRank, rightKnown := modelProviderSortRank(rows[j].Provider, catalog)
		switch {
		case leftKnown && rightKnown && leftRank != rightRank:
			return leftRank < rightRank
		case leftKnown && !rightKnown:
			return true
		case !leftKnown && rightKnown:
			return false
		default:
			return rows[i].Provider < rows[j].Provider
		}
	})
}

func modelProviderSortRank(name string, catalog cliSetupCatalog) (int, bool) {
	name = strings.TrimSpace(name)
	if name == "default" {
		return -1, true
	}
	for i, profile := range catalog.ProviderProfiles() {
		if profile.ID == name {
			return i, true
		}
	}
	return 0, false
}

func normalizeModelConfigSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "yaml"
	}
	return source
}

func effectiveModelProviderAPI(entry model.ProviderEntry) model.ProviderAPI {
	if api := model.NormalizeProviderAPI(entry.API); api != "" {
		return api
	}
	return model.APIOpenAICompletions
}

func configuredDurationString(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	return value.String()
}

func cloneModelHeaders(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
