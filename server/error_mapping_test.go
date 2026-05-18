package server

import (
	"errors"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestServerHTTPStatusForError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "run rejected conflict", err: agent.ErrRunRejected, want: http.StatusConflict},
		{name: "rate limited", err: runtimesvc.ErrRateLimited, want: http.StatusTooManyRequests},
		{name: "invalid text fallback", err: errors.New("invalid request payload"), want: http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := serverHTTPStatusForError(tc.err, http.StatusInternalServerError); got != tc.want {
				t.Fatalf("serverHTTPStatusForError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestMapToWSErrorUsesTypedClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "not found", err: errors.New("run not found"), want: WSErrNotFound},
		{name: "invalid argument", err: errors.New("invalid request payload"), want: WSErrInvalidRequest},
		{name: "rate limited", err: runtimesvc.ErrRateLimited, want: WSErrRateLimited},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapToWSError(tc.err)
			if got == nil {
				t.Fatalf("mapToWSError(%v) = nil", tc.err)
			}
			if got.Code != tc.want {
				t.Fatalf("mapToWSError(%v).Code = %q, want %q", tc.err, got.Code, tc.want)
			}
		})
	}
}
