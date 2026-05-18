package gateway

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/controlplane"
	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

func decodeOperatorJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeGatewayJSONBody(w, r, dst)
}

func decodeOperatorStrictJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeGatewayJSONBodyDisallowUnknownFields(w, r, dst)
}

func decodeOptionalGatewayJSONBody(w http.ResponseWriter, r *http.Request, dst any) (bool, bool) {
	return decodeOptionalGatewayJSONBodyWithOptions(w, r, dst, false)
}

func decodeOptionalGatewayJSONBodyDisallowUnknownFields(w http.ResponseWriter, r *http.Request, dst any) (bool, bool) {
	return decodeOptionalGatewayJSONBodyWithOptions(w, r, dst, true)
}

func decodeOptionalGatewayJSONBodyWithOptions(w http.ResponseWriter, r *http.Request, dst any, disallowUnknownFields bool) (bool, bool) {
	var (
		hasBody bool
		err     error
	)
	if disallowUnknownFields {
		hasBody, err = decodeOptionalJSONBodyDisallowUnknownFields(w, r, dst)
	} else {
		hasBody, err = decodeOptionalJSONBody(w, r, dst)
	}
	if !handleGatewayJSONDecodeError(w, err) {
		return false, false
	}
	return hasBody, true
}

func decodeGatewayJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeGatewayJSONBodyWithOptions(w, r, dst, false)
}

func decodeGatewayJSONBodyDisallowUnknownFields(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeGatewayJSONBodyWithOptions(w, r, dst, true)
}

func decodeGatewayJSONBodyWithOptions(w http.ResponseWriter, r *http.Request, dst any, disallowUnknownFields bool) bool {
	var err error
	if disallowUnknownFields {
		err = decodeJSONBodyDisallowUnknownFields(w, r, dst)
	} else {
		err = decodeJSONBody(w, r, dst)
	}
	return handleGatewayJSONDecodeError(w, err)
}

func handleGatewayJSONDecodeError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, errRequestBodyTooLarge) {
		gwErrorCode(w, http.StatusRequestEntityTooLarge, apiresponse.ErrorCodeRequestBodyTooLarge, "request body too large")
		return false
	}
	gwErrorCode(w, http.StatusBadRequest, apiresponse.ErrorCodeInvalidJSON, "invalid json: "+err.Error())
	return false
}

// triggerConfigReload notifies the config watcher to reload.
// This is a no-op if no watcher is configured.
func (g *Gateway) triggerConfigReload() error {
	if g == nil {
		return nil
	}
	if g.configWatcher != nil {
		if err := g.configWatcher.Reload(); err != nil {
			log.Warn("config reload failed", "error", err)
			return err
		}
		return nil
	}
	if g.configReload != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := g.configReload(ctx); err != nil {
			log.Warn("effective config refresh failed", "error", err)
			return err
		}
	}
	return nil
}

func cloneBoolPtrGateway(value *bool) *bool {
	if value == nil {
		return nil
	}
	v := *value
	return &v
}

func ensureObjectMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok && typed != nil {
		return typed
	}
	return make(map[string]any)
}

func httpStatusForConfigMutation(err error) int {
	switch err {
	case nil:
		return http.StatusOK
	case controlplane.ErrMutationUnavailable, controlplane.ErrEffectiveConfigUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}
