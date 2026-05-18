package gateway

import (
	"net/http"

	"github.com/fulcrus/hopclaw/deviceauth"
)

func (g *Gateway) handleDevicesList(w http.ResponseWriter, _ *http.Request) {
	if !g.requireDeviceAuth(w) {
		return
	}
	gwJSON(w, http.StatusOK, buildDevicesListResponse(g.deviceStore, g.devicePairing))
}

func (g *Gateway) handleDevicesPairCreate(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceAuth(w) {
		return
	}
	req, ok := decodeDevicePairCreateRequest(w, r)
	if !ok {
		return
	}

	if err := registerDeviceForPairCreate(g.deviceStore, req); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}

	rec, err := g.devicePairing.InitiatePairing(req.Channel, req.DeviceID)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	gwJSON(w, http.StatusCreated, buildDevicePairCreateResponse(rec))
}

func (g *Gateway) handleDevicesPairApprove(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceAuth(w) {
		return
	}
	req, role, expiresAt, ok := decodeDevicePairApproveRequest(w, r)
	if !ok {
		return
	}

	rec, err := g.devicePairing.VerifyCode(req.Code)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := g.issueDeviceToken(rec.DeviceID, role, req.Scopes, expiresAt)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, buildDevicePairApproveResponse(rec, role, req.Scopes, token))
}

func (g *Gateway) handleDevicePairClaim(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceAuth(w) {
		return
	}
	req, expiresAt, ok := decodeDevicePairClaimRequest(w, r)
	if !ok {
		return
	}
	rec, ok := loadDevicePairClaimRecord(w, g.devicePairing, req)
	if !ok {
		return
	}
	// Unauthenticated endpoint: always assign RoleNode to prevent privilege escalation.
	role := deviceauth.RoleNode
	if err := registerDeviceForPairClaim(g.deviceStore, rec.DeviceID, req); err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := g.devicePairing.VerifyCode(req.Code)
	if err != nil {
		gwError(w, http.StatusBadRequest, err.Error())
		return
	}
	token, err := g.issueDeviceToken(rec.DeviceID, role, req.Scopes, expiresAt)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, buildDevicePairClaimResponse(r, rec, role, req.Scopes, token))
}

func (g *Gateway) handleDevicesPairReject(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceAuth(w) {
		return
	}
	req, ok := decodeDevicePairRejectRequest(w, r)
	if !ok {
		return
	}

	channel, deviceID, status, err := resolvePairRejectTarget(g.devicePairing, req)
	if err != nil {
		gwError(w, status, err.Error())
		return
	}
	if err := g.devicePairing.RevokePairing(channel, deviceID); err != nil {
		gwError(w, deviceAuthErrorStatus(err), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, channelDeviceOKResponse{OK: true, DeviceID: deviceID, Channel: channel})
}

func (g *Gateway) handleDevicesTrust(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceStore(w) {
		return
	}
	deviceID, ok := requiredDeviceIDFromPath(w, r)
	if !ok {
		return
	}
	if err := g.deviceStore.TrustDevice(deviceID); err != nil {
		gwError(w, deviceAuthErrorStatus(err), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, deviceTrustResponse{OK: true, DeviceID: deviceID, Trusted: true})
}

func (g *Gateway) handleDevicesRevoke(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceStore(w) {
		return
	}
	deviceID, ok := requiredDeviceIDFromPath(w, r)
	if !ok {
		return
	}
	if err := g.deviceStore.RevokeDevice(deviceID); err != nil {
		gwError(w, deviceAuthErrorStatus(err), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, deviceTrustResponse{OK: true, DeviceID: deviceID, Trusted: false})
}

func (g *Gateway) handleDevicesTokenRotate(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceStore(w) {
		return
	}
	deviceID, ok := requiredDeviceIDFromPath(w, r)
	if !ok {
		return
	}
	if !ensureStoredDevice(w, g.deviceStore, deviceID) {
		return
	}
	req, role, expiresAt, ok := decodeDeviceTokenMutateRequest(w, r)
	if !ok {
		return
	}
	token, err := g.issueDeviceToken(deviceID, role, req.Scopes, expiresAt)
	if err != nil {
		gwError(w, http.StatusInternalServerError, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, buildDeviceTokenIssueResponse(deviceID, role, req.Scopes, token))
}

func (g *Gateway) handleDevicesTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if !g.requireDeviceStore(w) {
		return
	}
	deviceID, ok := requiredDeviceIDFromPath(w, r)
	if !ok {
		return
	}
	_, role, _, ok := decodeDeviceTokenMutateRequest(w, r)
	if !ok {
		return
	}
	if err := g.deviceStore.DeleteToken(deviceID, role); err != nil {
		gwError(w, deviceAuthErrorStatus(err), err.Error())
		return
	}
	gwJSON(w, http.StatusOK, deviceRoleResponse{OK: true, DeviceID: deviceID, Role: string(role)})
}
