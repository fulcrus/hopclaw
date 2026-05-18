package gateway

import (
	"errors"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/deviceauth"
)

func (g *Gateway) requireDeviceAuth(w http.ResponseWriter) bool {
	if g.deviceStore == nil || g.devicePairing == nil {
		gwError(w, http.StatusServiceUnavailable, "device auth not configured")
		return false
	}
	return true
}

func (g *Gateway) requireDeviceStore(w http.ResponseWriter) bool {
	if g.deviceStore == nil {
		gwError(w, http.StatusServiceUnavailable, "device auth not configured")
		return false
	}
	return true
}

func requiredDeviceIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	deviceID := strings.TrimSpace(r.PathValue("id"))
	if deviceID == "" {
		gwError(w, http.StatusBadRequest, "device id is required")
		return "", false
	}
	return deviceID, true
}

func deviceAuthErrorStatus(err error) int {
	if errors.Is(err, deviceauth.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func parseDeviceRole(raw string, fallback deviceauth.DeviceRole) (deviceauth.DeviceRole, error) {
	role := deviceauth.DeviceRole(strings.TrimSpace(raw))
	if role == "" {
		role = fallback
	}
	if !deviceauth.IsValidRole(role) {
		return "", errors.New("invalid role")
	}
	return role, nil
}

func decodeDevicePairCreateRequest(w http.ResponseWriter, r *http.Request) (devicePairCreateRequest, bool) {
	var req devicePairCreateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return devicePairCreateRequest{}, false
	}
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	req.Channel = strings.TrimSpace(req.Channel)
	if req.DeviceID == "" || req.Channel == "" {
		gwError(w, http.StatusBadRequest, "device_id and channel are required")
		return devicePairCreateRequest{}, false
	}
	return req, true
}

func decodeDevicePairApproveRequest(w http.ResponseWriter, r *http.Request) (devicePairApproveRequest, deviceauth.DeviceRole, time.Time, bool) {
	var req devicePairApproveRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return devicePairApproveRequest{}, "", time.Time{}, false
	}
	req.Code = strings.TrimSpace(req.Code)
	if req.Code == "" {
		gwError(w, http.StatusBadRequest, "code is required")
		return devicePairApproveRequest{}, "", time.Time{}, false
	}
	role, err := parseDeviceRole(req.Role, deviceauth.RoleViewer)
	if err != nil {
		gwError(w, http.StatusBadRequest, "invalid role")
		return devicePairApproveRequest{}, "", time.Time{}, false
	}
	expiresAt, err := parseOptionalTime(req.ExpiresAt)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return devicePairApproveRequest{}, "", time.Time{}, false
	}
	return req, role, expiresAt, true
}

func decodeDevicePairClaimRequest(w http.ResponseWriter, r *http.Request) (devicePairClaimRequest, time.Time, bool) {
	var req devicePairClaimRequest
	if !decodeGatewayJSONBody(w, r, &req) {
		return devicePairClaimRequest{}, time.Time{}, false
	}
	req.Code = strings.TrimSpace(req.Code)
	req.DeviceID = strings.TrimSpace(req.DeviceID)
	if req.Code == "" {
		gwError(w, http.StatusBadRequest, "code is required")
		return devicePairClaimRequest{}, time.Time{}, false
	}
	expiresAt, err := parseOptionalTime(req.ExpiresAt)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return devicePairClaimRequest{}, time.Time{}, false
	}
	return req, expiresAt, true
}

func loadDevicePairClaimRecord(w http.ResponseWriter, pairing *deviceauth.PairingManager, req devicePairClaimRequest) (*deviceauth.PairingRecord, bool) {
	rec, ok := pairing.GetByCode(req.Code)
	if !ok || rec == nil {
		gwError(w, http.StatusNotFound, "pairing code not found")
		return nil, false
	}
	if req.DeviceID != "" && req.DeviceID != rec.DeviceID {
		gwError(w, http.StatusBadRequest, "device_id does not match pairing")
		return nil, false
	}
	return rec, true
}

func decodeDevicePairRejectRequest(w http.ResponseWriter, r *http.Request) (devicePairRejectRequest, bool) {
	var req devicePairRejectRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return devicePairRejectRequest{}, false
	}
	return req, true
}

func decodeDeviceTokenMutateRequest(w http.ResponseWriter, r *http.Request) (deviceTokenMutateRequest, deviceauth.DeviceRole, time.Time, bool) {
	var req deviceTokenMutateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return deviceTokenMutateRequest{}, "", time.Time{}, false
	}
	role, err := parseDeviceRole(req.Role, "")
	if err != nil {
		gwError(w, http.StatusBadRequest, "invalid role")
		return deviceTokenMutateRequest{}, "", time.Time{}, false
	}
	expiresAt, err := parseOptionalTime(req.ExpiresAt)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return deviceTokenMutateRequest{}, "", time.Time{}, false
	}
	return req, role, expiresAt, true
}

func ensureStoredDevice(w http.ResponseWriter, store *deviceauth.Store, deviceID string) bool {
	if _, ok := store.GetDevice(deviceID); !ok {
		gwError(w, http.StatusNotFound, "device not found")
		return false
	}
	return true
}

func registerDeviceForPairCreate(store *deviceauth.Store, req devicePairCreateRequest) error {
	return store.RegisterDevice(&deviceauth.DeviceIdentity{
		DeviceID:     strings.TrimSpace(req.DeviceID),
		Name:         strings.TrimSpace(req.Name),
		Platform:     strings.TrimSpace(req.Platform),
		DeviceFamily: strings.TrimSpace(req.DeviceFamily),
		Trusted:      false,
	})
}

func registerDeviceForPairClaim(store *deviceauth.Store, deviceID string, req devicePairClaimRequest) error {
	return store.RegisterDevice(&deviceauth.DeviceIdentity{
		DeviceID:     strings.TrimSpace(deviceID),
		Name:         strings.TrimSpace(req.Name),
		Platform:     strings.TrimSpace(req.Platform),
		DeviceFamily: strings.TrimSpace(req.DeviceFamily),
		Trusted:      false,
	})
}

func resolvePairRejectTarget(pairing *deviceauth.PairingManager, req devicePairRejectRequest) (string, string, int, error) {
	channel := strings.TrimSpace(req.Channel)
	deviceID := strings.TrimSpace(req.DeviceID)
	code := strings.TrimSpace(req.Code)
	if code != "" {
		for _, rec := range pairing.ListPairings() {
			if rec != nil && rec.Code == code {
				return rec.Channel, rec.DeviceID, 0, nil
			}
		}
		return "", "", http.StatusNotFound, errors.New("pairing code not found")
	}
	if channel == "" || deviceID == "" {
		return "", "", http.StatusBadRequest, errors.New("channel and device_id are required")
	}
	return channel, deviceID, 0, nil
}

func devicePairWSURL(r *http.Request) string {
	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	return scheme + "://" + r.Host + requestExternalPathPrefix(r, devicePairClaimPath) + operatorWebSocketPath
}

func requestExternalPathPrefix(r *http.Request, routePath string) string {
	if r == nil {
		return ""
	}
	if prefix := normalizeForwardedPathPrefix(r.Header.Get("X-Forwarded-Prefix")); prefix != "" {
		return prefix
	}
	requestPath := cleanedRequestPath(r.URL.Path)
	routePath = cleanedRequestPath(routePath)
	if requestPath == "" || routePath == "" || !strings.HasSuffix(requestPath, routePath) {
		return ""
	}
	prefix := strings.TrimSuffix(requestPath, routePath)
	if prefix == "" || prefix == "/" {
		return ""
	}
	return strings.TrimRight(prefix, "/")
}

func normalizeForwardedPathPrefix(raw string) string {
	value := strings.TrimSpace(strings.Split(raw, ",")[0])
	if value == "" {
		return ""
	}
	clean := cleanedRequestPath(value)
	if clean == "" || clean == "/" {
		return ""
	}
	return strings.TrimRight(clean, "/")
}

func cleanedRequestPath(raw string) string {
	clean := path.Clean("/" + strings.TrimSpace(raw))
	if clean == "." {
		return ""
	}
	return clean
}

func (g *Gateway) issueDeviceToken(deviceID string, role deviceauth.DeviceRole, scopes []string, expiresAt time.Time) (*deviceauth.DeviceToken, error) {
	tokenValue, err := deviceauth.GenerateToken()
	if err != nil {
		return nil, err
	}
	token := &deviceauth.DeviceToken{
		Token:     tokenValue,
		DeviceID:  strings.TrimSpace(deviceID),
		Role:      role,
		Scopes:    append([]string(nil), scopes...),
		IssuedAt:  time.Now().UTC(),
		ExpiresAt: expiresAt,
	}
	if err := g.deviceStore.SetToken(token); err != nil {
		return nil, err
	}
	return token, nil
}

func buildDevicePairCreateResponse(rec *deviceauth.PairingRecord) devicePairCreateResponse {
	return devicePairCreateResponse{
		DeviceID:   rec.DeviceID,
		Channel:    rec.Channel,
		Code:       rec.Code,
		Status:     string(rec.Status),
		CreatedAt:  formatTime(rec.CreatedAt),
		ExpiresAt:  formatTime(rec.ExpiresAt),
		VerifiedAt: formatTime(rec.VerifiedAt),
	}
}

func buildDevicePairApproveResponse(rec *deviceauth.PairingRecord, role deviceauth.DeviceRole, scopes []string, token *deviceauth.DeviceToken) devicePairApproveResponse {
	return devicePairApproveResponse{
		OK:           true,
		DeviceID:     rec.DeviceID,
		Channel:      rec.Channel,
		Status:       string(rec.Status),
		Role:         string(role),
		Scopes:       append([]string(nil), scopes...),
		Token:        token.Token,
		TokenPreview: tokenPreview(token.Token),
		ExpiresAt:    formatTime(token.ExpiresAt),
	}
}

func buildDevicePairClaimResponse(r *http.Request, rec *deviceauth.PairingRecord, role deviceauth.DeviceRole, scopes []string, token *deviceauth.DeviceToken) devicePairClaimResponse {
	return devicePairClaimResponse{
		OK:           true,
		DeviceID:     rec.DeviceID,
		Channel:      rec.Channel,
		Role:         string(role),
		Scopes:       append([]string(nil), scopes...),
		Token:        token.Token,
		TokenPreview: tokenPreview(token.Token),
		ExpiresAt:    formatTime(token.ExpiresAt),
		WSURL:        devicePairWSURL(r),
	}
}

func buildDeviceTokenIssueResponse(deviceID string, role deviceauth.DeviceRole, scopes []string, token *deviceauth.DeviceToken) deviceTokenIssueResponse {
	return deviceTokenIssueResponse{
		OK:           true,
		DeviceID:     deviceID,
		Role:         string(role),
		Scopes:       append([]string(nil), scopes...),
		Token:        token.Token,
		TokenPreview: tokenPreview(token.Token),
		ExpiresAt:    formatTime(token.ExpiresAt),
	}
}

func buildDevicesListResponse(store *deviceauth.Store, pairing *deviceauth.PairingManager) devicesListResponse {
	itemsByID := make(map[string]*deviceSummary)
	for _, dev := range store.ListDevices() {
		if dev == nil {
			continue
		}
		itemsByID[dev.DeviceID] = &deviceSummary{
			DeviceID:     dev.DeviceID,
			Name:         dev.Name,
			Platform:     dev.Platform,
			DeviceFamily: dev.DeviceFamily,
			Trusted:      dev.Trusted,
			CreatedAt:    formatTime(dev.CreatedAt),
			LastSeenAt:   formatTime(dev.LastSeenAt),
		}
	}

	for _, token := range store.ListTokens("") {
		if token == nil {
			continue
		}
		summary := ensureDeviceSummary(itemsByID, token.DeviceID)
		summary.Tokens = append(summary.Tokens, deviceTokenSummary{
			DeviceID:     token.DeviceID,
			Role:         string(token.Role),
			Scopes:       append([]string(nil), token.Scopes...),
			TokenPreview: tokenPreview(token.Token),
			IssuedAt:     formatTime(token.IssuedAt),
			ExpiresAt:    formatTime(token.ExpiresAt),
			UpdatedAt:    formatTime(token.UpdatedAt),
		})
	}

	for _, rec := range pairing.ListPairings() {
		if rec == nil {
			continue
		}
		summary := ensureDeviceSummary(itemsByID, rec.DeviceID)
		summary.Pairings = append(summary.Pairings, devicePairingSummary{
			DeviceID:   rec.DeviceID,
			Channel:    rec.Channel,
			Code:       rec.Code,
			Status:     string(rec.Status),
			CreatedAt:  formatTime(rec.CreatedAt),
			ExpiresAt:  formatTime(rec.ExpiresAt),
			VerifiedAt: formatTime(rec.VerifiedAt),
		})
	}

	items := make([]deviceSummary, 0, len(itemsByID))
	for _, item := range itemsByID {
		sort.Slice(item.Tokens, func(i, j int) bool { return item.Tokens[i].Role < item.Tokens[j].Role })
		sort.Slice(item.Pairings, func(i, j int) bool {
			if item.Pairings[i].Status != item.Pairings[j].Status {
				return item.Pairings[i].Status < item.Pairings[j].Status
			}
			return item.Pairings[i].Channel < item.Pairings[j].Channel
		})
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DeviceID < items[j].DeviceID })
	return devicesListResponse{Items: items, Count: len(items)}
}

func ensureDeviceSummary(itemsByID map[string]*deviceSummary, deviceID string) *deviceSummary {
	deviceID = strings.TrimSpace(deviceID)
	if item, ok := itemsByID[deviceID]; ok {
		return item
	}
	item := &deviceSummary{DeviceID: deviceID}
	itemsByID[deviceID] = item
	return item
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func parseOptionalTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, errors.New("expires_at must be RFC3339")
	}
	return ts.UTC(), nil
}

func tokenPreview(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 12 {
		return token
	}
	return token[:10] + "..."
}
