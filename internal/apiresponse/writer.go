package apiresponse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/fulcrus/hopclaw/logging"
)

func WriteJSON(ctx context.Context, w http.ResponseWriter, status int, payload any, logMessage string) {
	if ctx == nil {
		ctx = context.Background()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	logging.LogIfErr(ctx, json.NewEncoder(w).Encode(payload), logMessage)
}

func WriteError(ctx context.Context, w http.ResponseWriter, status int, err error, logMessage string) {
	WriteErrorWithCode(ctx, w, status, "", err, logMessage)
}

func WriteErrorWithCode(ctx context.Context, w http.ResponseWriter, status int, code ErrorCode, err error, logMessage string) {
	if err == nil {
		err = errors.New("request failed")
	}
	if code == "" {
		code = DefaultHTTPErrorCode(status)
	}
	WriteJSON(ctx, w, status, Error{
		Code:  string(code),
		Error: strings.TrimSpace(err.Error()),
	}, logMessage)
}
