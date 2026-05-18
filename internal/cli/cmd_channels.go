package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	channelsBasePath        = "/operator/channels"
	channelsHealthPath      = "/operator/channels/health"
	channelsEventsPath      = "/runtime/events"
	channelsDefaultLogLimit = 20
)

// ---------------------------------------------------------------------------
// Response types (mirror the API JSON shapes)
// ---------------------------------------------------------------------------

type channelStatusResponse struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Connected bool   `json:"connected"`
	Message   string `json:"message,omitempty"`
}

type channelCreateRequest struct {
	Name    string         `json:"name"`
	Config  map[string]any `json:"config,omitempty"`
	Enabled *bool          `json:"enabled,omitempty"`
}

type channelAddResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type channelDeleteResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

type channelValidateResponse struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message"`
	Status  string `json:"status,omitempty"`
}

type channelTestMessageRequest struct {
	Channel  string `json:"channel"`
	TargetID string `json:"target_id,omitempty"`
	Message  string `json:"message"`
}

type channelTestMessageResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type channelLogEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Channel   string         `json:"channel,omitempty"`
	RunID     string         `json:"run_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Time      time.Time      `json:"time"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

type channelLogsResponse struct {
	Items []channelLogEvent `json:"items"`
}

type channelConfigInfo struct {
	Name    string          `json:"name"`
	Config  json.RawMessage `json:"config"`
	Enabled *bool           `json:"enabled,omitempty"`
	Source  string          `json:"source,omitempty"`
}

type channelConfigListResponse struct {
	Items []channelConfigInfo `json:"items"`
	Count int                 `json:"count"`
}

type channelHealthItem struct {
	Name         string `json:"name"`
	State        string `json:"state"`
	LastError    string `json:"last_error,omitempty"`
	RestartCount int    `json:"restart_count,omitempty"`
	ActiveRuns   int    `json:"active_runs,omitempty"`
}

type channelHealthListResponse struct {
	Items []channelHealthItem `json:"items"`
	Count int                 `json:"count"`
}

type channelListRow struct {
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Source     string `json:"source,omitempty"`
	Status     string `json:"status,omitempty"`
	Message    string `json:"message,omitempty"`
}

type channelCommandOptions struct {
	Name        string
	Set         []string
	Interactive bool
	Disabled    bool
	Token       string
}

// ---------------------------------------------------------------------------
// Parent command
// ---------------------------------------------------------------------------

func newChannelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channels",
		Short: "Inspect and manage configured channels",
		Long:  "List configured channel integrations, check runtime health, and inspect recent channel activity.",
	}

	cmd.AddCommand(
		newChannelsListCmd(),
		newChannelsStatusCmd(),
		newChannelsValidateCmd(),
		newChannelsTestCmd(),
		newChannelsAddCmd(),
		newChannelsRemoveCmd(),
		newChannelsLogsCmd(),
	)

	return cmd
}

// ---------------------------------------------------------------------------
// channels list
// ---------------------------------------------------------------------------

func newChannelsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured channels",
		Long:  "List configured channels from the operator config store and enrich them with runtime health when available.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChannelsList(cmd.Context())
		},
	}
}

func runChannelsList(ctx context.Context) error {
	client, _ := NewGatewayClient()
	rows, loadErr := loadOperatorChannelRows(ctx, client)
	if loadErr != nil {
		rows, loadErr = loadConfiguredChannels()
		if loadErr != nil {
			return loadErr
		}
	}

	health := map[string]channelHealthItem{}
	var healthResp channelHealthListResponse
	if client != nil && client.Get(ctx, channelsHealthPath, &healthResp) == nil {
		for _, item := range healthResp.Items {
			health[item.Name] = item
		}
	}

	for i := range rows {
		if item, ok := health[rows[i].Name]; ok {
			rows[i].Status = item.State
			rows[i].Message = item.LastError
			if rows[i].Message == "" && item.ActiveRuns > 0 {
				rows[i].Message = fmt.Sprintf("%d active runs", item.ActiveRuns)
			}
		} else if rows[i].Configured {
			rows[i].Status = "configured"
		} else {
			rows[i].Status = "not_configured"
		}
	}

	if flagJSON {
		return printJSON(rows)
	}
	return printChannelRows(rows)
}

// ---------------------------------------------------------------------------
// channels validate / test
// ---------------------------------------------------------------------------

func newChannelsValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <name>",
		Short: "Validate an existing channel adapter",
		Long:  "Ask the running gateway to reconnect an existing channel adapter and report the validation result.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChannelsValidate(cmd.Context(), args[0])
		},
	}
}

func runChannelsValidate(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	return runChannelsValidateWithClient(ctx, client, name)
}

func runChannelsValidateWithClient(ctx context.Context, client *GatewayClient, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("channel name is required")
	}

	var resp channelValidateResponse
	if err := client.Post(ctx, channelsBasePath+"/validate", map[string]string{
		"channel": name,
	}, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Channel: %s\n", name)
	fmt.Printf("Valid:   %v\n", resp.Valid)
	if resp.Status != "" {
		fmt.Printf("Status:  %s\n", resp.Status)
	}
	if resp.Message != "" {
		fmt.Printf("Message: %s\n", resp.Message)
	}
	return nil
}

func newChannelsTestCmd() *cobra.Command {
	var (
		message  string
		targetID string
	)
	cmd := &cobra.Command{
		Use:   "test <name>",
		Short: "Send a test message through a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChannelsTest(cmd.Context(), args[0], targetID, message)
		},
	}
	cmd.Flags().StringVar(&message, "message", "HopClaw test message", "message body to send")
	cmd.Flags().StringVar(&targetID, "target", "", "channel-specific target id such as a room, thread, or user")
	_ = cmd.MarkFlagRequired("target")
	return cmd
}

func runChannelsTest(ctx context.Context, name, targetID, message string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	return runChannelsTestWithClient(ctx, client, name, targetID, message)
}

func runChannelsTestWithClient(ctx context.Context, client *GatewayClient, name, targetID, message string) error {
	req := channelTestMessageRequest{
		Channel:  strings.TrimSpace(name),
		TargetID: strings.TrimSpace(targetID),
		Message:  message,
	}
	var resp channelTestMessageResponse
	if err := client.Post(ctx, channelsBasePath+"/test-message", req, &resp); err != nil {
		return err
	}
	if flagJSON {
		return printJSON(resp)
	}
	fmt.Printf("Channel: %s\n", name)
	fmt.Printf("Target:  %s\n", req.TargetID)
	fmt.Printf("Status:  %v\n", resp.OK)
	fmt.Printf("Result:  %s\n", resp.Message)
	return nil
}

// ---------------------------------------------------------------------------
// channels status
// ---------------------------------------------------------------------------

func newChannelsStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status [name]",
		Short: "Show channel health status",
		Long:  "Show health status for a specific channel or all channels.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) > 0 {
				name = args[0]
			}
			return runChannelsStatus(cmd.Context(), name)
		},
	}
}

func runChannelsStatus(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	if name != "" {
		return runChannelsStatusSingle(ctx, client, name)
	}
	return runChannelsStatusAll(ctx, client)
}

func runChannelsStatusSingle(ctx context.Context, client *GatewayClient, name string) error {
	var resp channelHealthListResponse
	if err := client.Get(ctx, channelsHealthPath, &resp); err != nil {
		return err
	}
	var item *channelHealthItem
	for i := range resp.Items {
		if resp.Items[i].Name == name {
			item = &resp.Items[i]
			break
		}
	}
	if item == nil {
		return fmt.Errorf("channel %q not found", name)
	}
	status := channelStatusResponse{
		Name:      item.Name,
		Status:    item.State,
		Connected: item.State == "connected",
		Message:   item.LastError,
	}

	if flagJSON {
		return printJSON(status)
	}

	fmt.Printf("Name:      %s\n", status.Name)
	fmt.Printf("Status:    %s\n", status.Status)
	fmt.Printf("Connected: %v\n", status.Connected)
	if status.Message != "" {
		fmt.Printf("Message:   %s\n", status.Message)
	}

	return nil
}

func runChannelsStatusAll(ctx context.Context, client *GatewayClient) error {
	var resp channelHealthListResponse
	if err := client.Get(ctx, channelsHealthPath, &resp); err != nil {
		return err
	}

	if flagJSON {
		var statuses []channelStatusResponse
		for _, r := range resp.Items {
			statuses = append(statuses, channelStatusResponse{
				Name:      r.Name,
				Status:    r.State,
				Connected: r.State == "connected",
				Message:   r.LastError,
			})
		}
		return printJSON(statuses)
	}

	if len(resp.Items) == 0 {
		fmt.Println("no channels found")
		return nil
	}

	fmt.Printf("%-28s  %-14s  %-10s  %s\n", "NAME", "STATUS", "CONNECTED", "MESSAGE")
	fmt.Printf("%-28s  %-14s  %-10s  %s\n", "----", "------", "---------", "-------")
	for _, r := range resp.Items {
		isConnected := r.State == "connected"
		msg := r.LastError
		if msg == "" {
			msg = "-"
		}
		fmt.Printf("%-28s  %-14s  %-10v  %s\n",
			truncate(r.Name, 28),
			r.State,
			isConnected,
			truncate(msg, 40),
		)
	}

	fmt.Printf("\nTotal: %d channels\n", len(resp.Items))
	return nil
}

func loadConfiguredChannels() ([]channelListRow, error) {
	p := resolveConfigPath()
	if p == "" {
		return nil, fmt.Errorf("no config file found; run 'hopclaw setup'")
	}
	cfg, err := config.Load(p)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return buildChannelRows(cfg.Channels), nil
}

func loadOperatorChannelRows(ctx context.Context, client *GatewayClient) ([]channelListRow, error) {
	if client == nil {
		return nil, fmt.Errorf("gateway client is not configured")
	}
	var resp channelConfigListResponse
	if err := client.Get(ctx, channelsBasePath, &resp); err != nil {
		return nil, err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	rows := make([]channelListRow, 0, len(resp.Items))
	for _, item := range resp.Items {
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		rows = append(rows, channelListRow{
			Name:       item.Name,
			Enabled:    enabled,
			Configured: true,
			Source:     normalizeChannelRowSource(item.Source),
			Status:     "configured",
		})
	}
	sortChannelRowsByCatalog(rows, catalog.ChannelProfiles())
	return rows, nil
}

func buildChannelRows(cfg config.ChannelsConfig) []channelListRow {
	value := reflect.ValueOf(cfg)
	typ := value.Type()
	rows := make([]channelListRow, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := yamlTagName(field)
		if name == "" {
			continue
		}
		rows = append(rows, buildChannelRow(name, value.Field(i)))
	}
	filtered := make([]channelListRow, 0, len(rows))
	for _, row := range rows {
		if row.Configured {
			filtered = append(filtered, row)
		}
	}
	sortChannelRowsByCatalog(filtered, config.ChannelProfiles())
	return filtered
}

func buildChannelRow(name string, value reflect.Value) channelListRow {
	row := channelListRow{
		Name:       name,
		Enabled:    channelEnabled(value),
		Configured: channelConfigured(name, value),
		Source:     "yaml",
		Status:     "configured",
	}
	return row
}

func channelEnabled(value reflect.Value) bool {
	value = indirectValue(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return true
	}
	field := value.FieldByName("Enabled")
	if !field.IsValid() || field.Kind() != reflect.Ptr || field.IsNil() {
		return true
	}
	return field.Elem().Bool()
}

func channelConfigured(name string, value reflect.Value) bool {
	value = indirectValue(value)
	if !value.IsValid() {
		return false
	}
	if profile, ok := config.LookupChannelProfile(name); ok {
		for _, field := range profile.Fields {
			if configuredFieldByConfigKey(value, field.ConfigKey) {
				return true
			}
		}
	}
	return structHasMeaningfulConfig(value)
}

func configuredFieldByConfigKey(value reflect.Value, configKey string) bool {
	fieldValue, ok := structFieldByYAMLTag(value, configKey)
	if !ok {
		return false
	}
	return configValueConfigured(fieldValue)
}

func structFieldByYAMLTag(value reflect.Value, configKey string) (reflect.Value, bool) {
	value = indirectValue(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		fieldValue := value.Field(i)
		if yamlTagName(field) == configKey {
			return fieldValue, true
		}
		if field.Anonymous {
			if nested, ok := structFieldByYAMLTag(fieldValue, configKey); ok {
				return nested, true
			}
		}
	}
	return reflect.Value{}, false
}

func structHasMeaningfulConfig(value reflect.Value) bool {
	value = indirectValue(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return configValueConfigured(value)
	}
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Name == "Enabled" {
			continue
		}
		if field.Anonymous && field.Type == reflect.TypeOf(config.CommonChannelConfig{}) {
			continue
		}
		if configValueConfigured(value.Field(i)) {
			return true
		}
	}
	return false
}

func configValueConfigured(value reflect.Value) bool {
	value = indirectValue(value)
	if !value.IsValid() {
		return false
	}
	switch value.Kind() {
	case reflect.String:
		return strings.TrimSpace(value.String()) != ""
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() != 0
	case reflect.Float32, reflect.Float64:
		return value.Float() != 0
	case reflect.Map, reflect.Slice, reflect.Array:
		return value.Len() > 0
	case reflect.Struct:
		return structHasMeaningfulConfig(value)
	default:
		return !value.IsZero()
	}
}

func indirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}

func yamlTagName(field reflect.StructField) string {
	tag := strings.TrimSpace(field.Tag.Get("yaml"))
	if tag == "" {
		return ""
	}
	name := strings.TrimSpace(strings.Split(tag, ",")[0])
	if name == "" || name == "-" || name == ",inline" {
		return ""
	}
	return name
}

func normalizeChannelRowSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "yaml"
	}
	return source
}

func sortChannelRowsByCatalog(rows []channelListRow, profiles []config.ChannelProfile) {
	if len(rows) < 2 {
		return
	}
	order := make(map[string]int, len(profiles))
	for i, profile := range profiles {
		order[profile.ID] = i
	}
	sort.SliceStable(rows, func(i, j int) bool {
		leftRank, leftOK := order[rows[i].Name]
		rightRank, rightOK := order[rows[j].Name]
		switch {
		case leftOK && rightOK:
			return leftRank < rightRank
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return rows[i].Name < rows[j].Name
		}
	})
}

func printChannelRows(rows []channelListRow) error {
	if len(rows) == 0 {
		fmt.Println("no configured channels found")
		return nil
	}
	fmt.Printf("%-18s  %-8s  %-10s  %-12s  %s\n", "NAME", "ENABLED", "CONFIGURED", "STATUS", "MESSAGE")
	fmt.Printf("%-18s  %-8s  %-10s  %-12s  %s\n", "----", "-------", "----------", "------", "-------")
	for _, row := range rows {
		message := row.Message
		if message == "" {
			message = "-"
		}
		fmt.Printf("%-18s  %-8v  %-10v  %-12s  %s\n",
			row.Name,
			row.Enabled,
			row.Configured,
			truncate(row.Status, 12),
			truncate(message, 50),
		)
	}
	fmt.Printf("\nTotal: %d channels\n", len(rows))
	return nil
}

// ---------------------------------------------------------------------------
// channels add
// ---------------------------------------------------------------------------

func newChannelsAddCmd() *cobra.Command {
	var opts channelCommandOptions

	cmd := &cobra.Command{
		Use:   "add <type>",
		Short: "Add a channel adapter",
		Long:  "Add a new channel adapter through the operator surface using schema-driven channel fields.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChannelsAdd(cmd.Context(), args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.Name, "name", "", "channel name; defaults to the canonical channel id")
	cmd.Flags().StringArrayVar(&opts.Set, "set", nil, "set channel fields as field=value; repeat as needed")
	cmd.Flags().BoolVar(&opts.Interactive, "interactive", false, "prompt for channel fields interactively using the setup catalog schema")
	cmd.Flags().BoolVar(&opts.Disabled, "disabled", false, "create the channel in a disabled state")
	cmd.Flags().StringVar(&opts.Token, "token", "", "legacy shorthand for the primary token field when the channel exposes one")

	return cmd
}

func runChannelsAdd(ctx context.Context, channelType string, opts channelCommandOptions) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}
	catalog := loadCLISetupCatalog(ctx, client)
	req, profile, err := buildChannelCreateRequest(catalog, channelType, opts)
	if err != nil {
		return err
	}
	return runChannelsAddWithClient(ctx, client, profile.ID, req)
}

func runChannelsAddWithClient(ctx context.Context, client *GatewayClient, channelType string, req channelCreateRequest) error {
	var resp channelAddResponse
	if err := client.Post(ctx, channelsBasePath, req, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Added channel %q (type: %s)\n", resp.Name, channelType)
	return nil
}

func buildChannelCreateRequest(catalog cliSetupCatalog, channelType string, opts channelCommandOptions) (channelCreateRequest, config.ChannelProfile, error) {
	profile, ok := catalog.LookupChannelProfile(channelType)
	if !ok {
		return channelCreateRequest{}, config.ChannelProfile{}, fmt.Errorf("unknown channel type %q", channelType)
	}

	fields := config.EffectiveOperatorChannelFields(profile)
	values := map[string]string{}
	if opts.Interactive {
		promptValues, err := promptChannelFieldValues(fields)
		if err != nil {
			return channelCreateRequest{}, config.ChannelProfile{}, err
		}
		for key, value := range promptValues {
			values[key] = value
		}
	}
	if err := applyChannelFieldAssignments(values, fields, opts.Set); err != nil {
		return channelCreateRequest{}, config.ChannelProfile{}, err
	}
	if err := applyLegacyChannelToken(values, fields, opts.Token); err != nil {
		return channelCreateRequest{}, config.ChannelProfile{}, err
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = profile.ID
	}
	enabled := !opts.Disabled
	req := channelCreateRequest{
		Name:    name,
		Config:  map[string]any{"type": profile.ID},
		Enabled: &enabled,
	}
	for _, field := range fields {
		value, ok, err := channelFieldValueFromInputs(values, field)
		if err != nil {
			return channelCreateRequest{}, config.ChannelProfile{}, fmt.Errorf("%s %s: %w", profile.DisplayName, field.Label, err)
		}
		if !ok {
			if field.Required {
				return channelCreateRequest{}, config.ChannelProfile{}, fmt.Errorf("%s requires %s", profile.DisplayName, field.Label)
			}
			continue
		}
		req.Config[field.ConfigKey] = value
	}
	return req, profile, nil
}

func promptChannelFieldValues(fields []config.SetupChannelField) (map[string]string, error) {
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		value, err := promptSetupChannelField(field)
		if err != nil {
			return nil, err
		}
		if value != "" {
			values[field.ID] = value
		}
	}
	return values, nil
}

func applyChannelFieldAssignments(values map[string]string, fields []config.SetupChannelField, raw []string) error {
	if len(raw) == 0 {
		return nil
	}
	fieldIndex := make(map[string]config.SetupChannelField, len(fields)*2)
	for _, field := range fields {
		fieldIndex[strings.TrimSpace(field.ID)] = field
		fieldIndex[strings.TrimSpace(field.ConfigKey)] = field
	}
	for _, item := range raw {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return fmt.Errorf("invalid channel field assignment %q; use field=value", item)
		}
		key = strings.TrimSpace(key)
		field, exists := fieldIndex[key]
		if !exists {
			return fmt.Errorf("unknown channel field %q", key)
		}
		values[field.ID] = strings.TrimSpace(value)
	}
	return nil
}

func applyLegacyChannelToken(values map[string]string, fields []config.SetupChannelField, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	field, ok := channelLegacyTokenField(fields)
	if !ok {
		return fmt.Errorf("this channel does not expose a token shorthand; use --set field=value instead")
	}
	if existing := strings.TrimSpace(values[field.ID]); existing != "" && existing != token {
		return fmt.Errorf("--token conflicts with %s; keep one source of truth", field.ID)
	}
	values[field.ID] = token
	return nil
}

func channelLegacyTokenField(fields []config.SetupChannelField) (config.SetupChannelField, bool) {
	preferred := []string{"bot_token", "channel_token", "api_token", "access_token", "auth_token", "token"}
	for _, id := range preferred {
		for _, field := range fields {
			if field.ID == id || field.ConfigKey == id {
				return field, true
			}
		}
	}
	return config.SetupChannelField{}, false
}

func channelFieldValueFromInputs(values map[string]string, field config.SetupChannelField) (any, bool, error) {
	raw := strings.TrimSpace(values[field.ID])
	if raw == "" {
		raw = strings.TrimSpace(values[field.ConfigKey])
	}
	if raw == "" {
		raw = strings.TrimSpace(field.DefaultValue)
	}
	if raw == "" {
		return nil, false, nil
	}
	switch field.Type {
	case config.SetupChannelFieldStringList:
		items := splitCLIChannelFieldList(raw)
		if len(items) == 0 {
			return nil, false, nil
		}
		return items, true, nil
	case config.SetupChannelFieldBool:
		value, err := parseCLIChannelBool(raw)
		if err != nil {
			return nil, false, err
		}
		return value, true, nil
	default:
		return raw, true, nil
	}
}

func parseCLIChannelBool(value string) (bool, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "1", "true", "yes", "on", "enabled":
		return true, nil
	case "0", "false", "no", "off", "disabled":
		return false, nil
	default:
		return false, fmt.Errorf("must be true or false")
	}
}

func splitCLIChannelFieldList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// channels remove
// ---------------------------------------------------------------------------

func newChannelsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a channel adapter",
		Long:  "Remove a channel adapter by name from the running gateway.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChannelsRemove(cmd.Context(), args[0])
		},
	}
}

func runChannelsRemove(ctx context.Context, name string) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	var resp channelDeleteResponse
	if err := client.Delete(ctx, channelsBasePath+"/"+url.PathEscape(strings.TrimSpace(name)), &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	fmt.Printf("Removed channel %q\n", name)
	return nil
}

// ---------------------------------------------------------------------------
// channels logs
// ---------------------------------------------------------------------------

func newChannelsLogsCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "View recent channel events",
		Long:  "View recent events filtered by channel name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChannelsLogs(cmd.Context(), args[0], limit)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", channelsDefaultLogLimit, "maximum number of events to return")

	return cmd
}

func runChannelsLogs(ctx context.Context, name string, limit int) error {
	client, err := NewGatewayClient()
	if err != nil {
		return err
	}

	path := fmt.Sprintf("%s?channel=%s&limit=%d", channelsEventsPath, url.QueryEscape(strings.TrimSpace(name)), limit)

	var resp channelLogsResponse
	if err := client.Get(ctx, path, &resp); err != nil {
		return err
	}

	if flagJSON {
		return printJSON(resp)
	}

	if len(resp.Items) == 0 {
		fmt.Printf("no events found for channel %q\n", name)
		return nil
	}

	fmt.Printf("%-12s  %-28s  %-12s  %-12s  %s\n",
		"ID", "TYPE", "RUN", "SESSION", "TIME")
	fmt.Printf("%-12s  %-28s  %-12s  %-12s  %s\n",
		"---", "----", "---", "-------", "----")
	for _, e := range resp.Items {
		runID := e.RunID
		if runID == "" {
			runID = "-"
		}
		sessionID := e.SessionID
		if sessionID == "" {
			sessionID = "-"
		}
		fmt.Printf("%-12s  %-28s  %-12s  %-12s  %s\n",
			truncate(e.ID, 12),
			truncate(e.Type, 28),
			truncate(runID, 12),
			truncate(sessionID, 12),
			formatTime(e.Time),
		)
	}

	return nil
}
