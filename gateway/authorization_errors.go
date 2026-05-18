package gateway

import (
	"encoding/json"
	"net/http"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

func writeAuthorizationError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Code:  string(apiresponse.ErrorCodeAuthorizationDenied),
		Error: msg,
	})
}
