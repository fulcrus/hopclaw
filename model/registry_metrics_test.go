package model

import (
	"errors"
	"testing"
)

func TestErrorClass(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("context canceled"), "canceled"},
		{errors.New("context deadline exceeded"), "timeout"},
		{errors.New("status 429 rate limit"), "rate_limited"},
		{errors.New("status 503 service unavailable"), "server_error"},
		{errors.New("status 401 unauthorized"), "auth_error"},
		{errors.New("something else"), "other"},
	}

	for _, test := range tests {
		if got := errorClass(test.err); got != test.want {
			t.Fatalf("errorClass(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}
