package gateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	apiresponse "github.com/fulcrus/hopclaw/internal/apiresponse"
)

func gwJSON(w http.ResponseWriter, status int, v any) {
	apiresponse.WriteJSON(context.Background(), w, status, v, "write gateway json response failed")
}

func gwError(w http.ResponseWriter, status int, message string) {
	gwErrorCode(w, status, "", message)
}

func gwErrorCode(w http.ResponseWriter, status int, code apiresponse.ErrorCode, message string) {
	if code == "" {
		code = apiresponse.DefaultHTTPErrorCode(status)
	}
	apiresponse.WriteJSON(context.Background(), w, status, errorResponse{
		Code:  string(code),
		Error: strings.TrimSpace(message),
	}, "write gateway json response failed")
}

func gwErrorf(w http.ResponseWriter, status int, format string, args ...any) {
	gwError(w, status, fmt.Sprintf(format, args...))
}
