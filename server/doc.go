// Package server exposes a small HTTP control plane for runs, approvals, and
// audit/event inspection.
package server

import "github.com/fulcrus/hopclaw/logging"

var log = logging.WithSubsystem("server")
