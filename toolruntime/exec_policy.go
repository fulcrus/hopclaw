package toolruntime

import (
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/config"
)

type cappedBuffer struct {
	limit     int
	buf       bytes.Buffer
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	if b.buf.Len() >= b.limit {
		b.truncated = true
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string {
	out := b.buf.String()
	if b.truncated {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "...[truncated]"
	}
	return out
}

func newExecCapture(limit int) (*cappedBuffer, *cappedBuffer, io.Writer, io.Writer) {
	stdout := &cappedBuffer{limit: limit}
	stderr := &cappedBuffer{limit: limit}
	return stdout, stderr, stdout, stderr
}

func (b *Builtins) effectiveExecTimeout(requested time.Duration) time.Duration {
	timeout := requested
	if timeout <= 0 {
		timeout = b.config.DefaultExecTimeout
	}
	if capTimeout := b.config.ExecConstraints.Timeout; capTimeout > 0 && (timeout <= 0 || timeout > capTimeout) {
		timeout = capTimeout
	}
	if timeout <= 0 {
		timeout = execToolFallback
	}
	return timeout
}

func (b *Builtins) execOutputLimit() int {
	if b.config.ExecConstraints.MaxOutput <= 0 {
		return 0
	}
	return b.config.ExecConstraints.MaxOutput
}

func (b *Builtins) validateExecInvocation(command string, args []string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("command is required")
	}
	return validateExecConstraintSet(execConstraintsAdapter{
		modeValue:      b.config.ExecConstraints.Mode,
		allowlistValue: b.config.ExecConstraints.Allowlist,
		denylistValue:  b.config.ExecConstraints.Denylist,
	}, buildExecCommandLine(command, args), command)
}

func (b *Builtins) validateShellInvocation(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("command is required")
	}
	return validateExecConstraintSet(execConstraintsAdapter{
		modeValue:      b.config.ExecConstraints.Mode,
		allowlistValue: b.config.ExecConstraints.Allowlist,
		denylistValue:  b.config.ExecConstraints.Denylist,
	}, command, command)
}

func validateExecConstraintSet(cfg anyExecConstraints, fullCommand, primary string) error {
	mode := strings.ToLower(strings.TrimSpace(cfg.mode()))
	if mode == "" {
		mode = "approve"
	}
	values := []string{strings.TrimSpace(fullCommand), strings.TrimSpace(primary)}
	if matchExecPatterns(cfg.denylist(), values...) {
		return fmt.Errorf("command %q is denied by exec.denylist", fullCommand)
	}
	switch mode {
	case "deny":
		return fmt.Errorf("exec tools are disabled by config")
	case "allowlist":
		if !matchExecPatterns(cfg.allowlist(), values...) {
			return fmt.Errorf("command %q is not permitted by exec.allowlist", fullCommand)
		}
	case "approve", "full":
		return nil
	default:
		return fmt.Errorf("unsupported exec mode %q", mode)
	}
	return nil
}

type anyExecConstraints interface {
	mode() string
	allowlist() []string
	denylist() []string
}

type execConstraintsAdapter struct {
	modeValue      string
	allowlistValue []string
	denylistValue  []string
}

var _ anyExecConstraints = execConstraintsAdapter{}
var _ = config.ExecConstraints{}

func (a execConstraintsAdapter) mode() string        { return a.modeValue }
func (a execConstraintsAdapter) allowlist() []string { return a.allowlistValue }
func (a execConstraintsAdapter) denylist() []string  { return a.denylistValue }

func buildExecCommandLine(command string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	if strings.TrimSpace(command) != "" {
		parts = append(parts, strings.TrimSpace(command))
	}
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

func matchExecPatterns(patterns []string, values ...string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if ok, err := filepath.Match(pattern, value); err == nil && ok {
				return true
			}
		}
	}
	return false
}
