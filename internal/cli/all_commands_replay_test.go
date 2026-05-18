package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	backupsvc "github.com/fulcrus/hopclaw/backup"
	"github.com/fulcrus/hopclaw/config"
	"github.com/spf13/cobra"
)

type replayKind string

type replayConfigMode string

const (
	replayExecuted replayKind = "executed"
	replayHelpOnly replayKind = "help_only"
	replayBlocked  replayKind = "blocked"

	replayConfigNone    replayConfigMode = "none"
	replayConfigDefault replayConfigMode = "default"
	replayConfigMinimal replayConfigMode = "minimal"
)

type replayCase struct {
	args         []string
	argsFn       func(*replayEnv) []string
	stdin        string
	configMode   replayConfigMode
	childEnv     map[string]string
	wantExit     int
	wantContains []string
	allowEmpty   bool
	prepare      func(*testing.T, *replayEnv)
	run          func(*testing.T, string, *replayEnv, *replayCase) ([]byte, int)
	verify       func(*testing.T, *replayEnv, []byte)
}

type replayEnv struct {
	root        string
	home        string
	workdir     string
	binDir      string
	configPath  string
	stateDir    string
	editorPath  string
	gatewayURL  string
	gatewayHost string
	values      map[string]string
}

func TestCLILeafReplayMatrixCoversAllCommands(t *testing.T) {
	root := newRootCmd()
	paths := collectCommandPaths(root, nil)
	if len(paths) == 0 {
		t.Fatal("no CLI command paths discovered")
	}

	for _, path := range paths {
		cmd := lookupCommand(root, path)
		if cmd == nil {
			t.Fatalf("lookupCommand(%q) returned nil", strings.Join(path, " "))
		}
		kind, _, reason := replayPlan(path, cmd)
		switch kind {
		case replayExecuted, replayHelpOnly, replayBlocked:
		default:
			t.Fatalf("command %q has no replay plan", strings.Join(path, " "))
		}
		if kind == replayBlocked && strings.TrimSpace(reason) == "" {
			t.Fatalf("blocked command %q is missing a reason", strings.Join(path, " "))
		}
	}
}

func TestCLILeafCommandsExecuteFromBuiltBinary(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-123456")

	bin := buildCLIIntegrationBinary(t)
	gateway := newCLIReplayGateway(t)
	defer gateway.Close()

	root := newRootCmd()
	paths := collectCommandPaths(root, nil)
	if len(paths) == 0 {
		t.Fatal("no CLI command paths discovered")
	}

	for _, path := range paths {
		path := path
		cmd := lookupCommand(root, path)
		if cmd == nil {
			t.Fatalf("lookupCommand(%q) returned nil", strings.Join(path, " "))
		}
		kind, plan, _ := replayPlan(path, cmd)
		if kind != replayExecuted || plan == nil {
			continue
		}

		t.Run(strings.Join(path, "_"), func(t *testing.T) {
			env := newReplayEnv(t, gateway.URL, plan.configMode)
			if plan.prepare != nil {
				plan.prepare(t, env)
			}

			args := replayArgs(plan, env)
			if len(args) == 0 {
				t.Fatalf("replay case %q produced no args", strings.Join(path, " "))
			}

			output, exitCode := runReplayCase(t, bin, env, plan)

			if exitCode != plan.wantExit {
				t.Fatalf("command %q exit code = %d, want %d\noutput=%s", strings.Join(args, " "), exitCode, plan.wantExit, string(output))
			}
			if !plan.allowEmpty && len(output) == 0 {
				t.Fatalf("command %q produced no output", strings.Join(args, " "))
			}
			for _, want := range plan.wantContains {
				if !strings.Contains(string(output), want) {
					t.Fatalf("command %q output missing %q\noutput=%s", strings.Join(args, " "), want, string(output))
				}
			}
			if plan.verify != nil {
				plan.verify(t, env, output)
			}
		})
	}
}

func replayPlan(path []string, cmd *cobra.Command) (replayKind, *replayCase, string) {
	if cmd != nil && len(cmd.Commands()) > 0 {
		return replayHelpOnly, nil, "parent command"
	}

	key := strings.Join(path, " ")
	if tc, ok := replayCases()[key]; ok {
		caseCopy := tc
		return replayExecuted, &caseCopy, ""
	}

	if reason := replayBlockedReason(path); reason != "" {
		return replayBlocked, nil, reason
	}
	return "", nil, ""
}

func replayBlockedReason(path []string) string {
	if len(path) == 0 {
		return ""
	}
	switch path[0] {
	case "channels":
		return "operator channel mutation/validation matrix is not yet hermetic in the binary replay harness"
	case "daemon":
		return "system service management touches launchd/systemd and is unsafe for hermetic replay"
	case "devices":
		return "device helper lifecycle needs companion daemons and a richer local handshake harness"
	case "doctor":
		return "doctor probes intentionally depend on host environment, disk, services, and provider connectivity"
	case "hooks":
		return "hook replay still needs a dedicated gateway fixture for hook schemas and result history"
	case "message":
		return "message flows need a dedicated async run/result harness beyond this replay slice"
	case "models":
		return "model operator and completion probes need a dedicated provider fixture and response matrix"
	case "secrets":
		return "secrets commands talk to the native platform keychain"
	case "serve":
		return "serve is a long-running gateway process rather than a bounded command"
	case "tui":
		return "tui launches the legacy gateway monitor TUI"
	case "uninstall":
		return "uninstall is destructive and removes binaries and state"
	case "update":
		return "update depends on live external release infrastructure"
	}
	if strings.Join(path, " ") == "logs stream" {
		return "logs stream is an unbounded SSE command rather than a bounded replay step"
	}
	return ""
}

func replayCases() map[string]replayCase {
	return map[string]replayCase{
		"agents add": {
			args:         []string{"--json", "agents", "add", "ops-bot", "--model", "gpt-5.4", "--description", "Ops bot"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "ops-bot"`},
		},
		"agents bind": {
			args:         []string{"--json", "agents", "bind", "ops-bot", "--channel", "slack", "--session-key", "cli"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"channel": "slack"`, `"agent": "ops-bot"`},
		},
		"agents delete": {
			args:         []string{"--json", "agents", "delete", "ops-bot"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "ops-bot"`},
		},
		"agents get": {
			args:         []string{"--json", "agents", "get", "ops-bot"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "ops-bot"`, `"system_prompt": "You are an ops agent."`},
		},
		"agents list": {
			args:         []string{"--json", "agents", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "ops-bot"`},
		},
		"approvals approve": {
			args:         []string{"--json", "approvals", "approve", "approval-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "approved"`, `"id": "approval-1"`},
		},
		"approvals deny": {
			args:         []string{"--json", "approvals", "deny", "approval-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "denied"`, `"id": "approval-1"`},
		},
		"approvals get": {
			args:         []string{"--json", "approvals", "get", "approval-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "approval-1"`, `"tool_calls"`},
		},
		"approvals list": {
			args:         []string{"--json", "approvals", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"approval-1"`, `"pending"`},
		},
		"automation create": {
			args:         []string{"--json", "automation", "create", "--name", "daily-report", "--schedule-kind", "every", "--every", "1h", "--content", "hello"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "cron-1"`, `"name": "daily-report"`},
		},
		"automation delete": {
			args:         []string{"--json", "automation", "delete", "cron-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"id": "cron-1"`},
		},
		"automation inspect": {
			args:         []string{"--json", "automation", "inspect", "cron", "cron-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "cron-1"`, `"recent_executions"`},
		},
		"automation list": {
			args:         []string{"--json", "automation", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"kind": "cron"`, `"name": "Daily report"`},
		},
		"automation pause": {
			args:         []string{"--json", "automation", "pause", "cron", "cron-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"enabled": false`},
		},
		"automation recent": {
			args:         []string{"--json", "automation", "recent", "cron", "cron-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"verification_status": "passed"`},
		},
		"automation resume": {
			args:         []string{"--json", "automation", "resume", "cron", "cron-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"enabled": true`},
		},
		"automation status": {
			args:         []string{"--json", "automation", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"cron"`, `"available": true`},
		},
		"automation templates": {
			args:         []string{"--json", "automation", "templates"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"daily-report-briefing"`, `"Daily Report Briefing"`},
		},
		"automation trigger": {
			args:         []string{"--json", "automation", "trigger", "cron-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"id": "cron-1"`},
		},
		"backup create": {
			args:       []string{"--json", "backup", "create"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := os.WriteFile(filepath.Join(env.stateDir, "notes.txt"), []byte("backup me"), 0o644); err != nil {
					t.Fatalf("WriteFile(notes.txt) error = %v", err)
				}
			},
			wantContains: []string{`"path":`, `.tar.gz`},
		},
		"backup list": {
			args:       []string{"--json", "backup", "list"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := os.WriteFile(filepath.Join(env.stateDir, "notes.txt"), []byte("backup me"), 0o644); err != nil {
					t.Fatalf("WriteFile(notes.txt) error = %v", err)
				}
				svc := backupsvc.NewService(env.stateDir)
				if _, err := svc.Create(t.Context()); err != nil {
					t.Fatalf("backup Create() error = %v", err)
				}
			},
			wantContains: []string{`.tar.gz`},
		},
		"backup restore": {
			argsFn: func(env *replayEnv) []string {
				return []string{"--json", "backup", "restore", env.values["backup_path"]}
			},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				original := filepath.Join(env.stateDir, "restore-me.txt")
				if err := os.WriteFile(original, []byte("restore target"), 0o644); err != nil {
					t.Fatalf("WriteFile(restore-me.txt) error = %v", err)
				}
				svc := backupsvc.NewService(env.stateDir)
				result, err := svc.Create(t.Context())
				if err != nil {
					t.Fatalf("backup Create() error = %v", err)
				}
				env.values["backup_path"] = result.Path
				if err := os.Remove(original); err != nil {
					t.Fatalf("Remove(restore-me.txt) error = %v", err)
				}
			},
			wantContains: []string{`"files_restored":`},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if _, err := os.Stat(filepath.Join(env.stateDir, "restore-me.txt")); err != nil {
					t.Fatalf("restored file missing: %v", err)
				}
			},
		},
		"browser close": {
			args:         []string{"--json", "browser", "close", "browser-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"session_id": "browser-1"`},
		},
		"browser open": {
			args:         []string{"--json", "browser", "open", "https://example.com"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"session_id": "browser-1"`},
		},
		"browser screenshot": {
			argsFn: func(env *replayEnv) []string {
				path := filepath.Join(env.root, "browser-shot.png")
				env.values["screenshot"] = path
				return []string{"--json", "browser", "screenshot", "browser-1", "--output", path}
			},
			configMode:   replayConfigDefault,
			wantContains: []string{`"path":`, `browser-shot.png`},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if info, err := os.Stat(env.values["screenshot"]); err != nil || info.Size() == 0 {
					t.Fatalf("screenshot file missing or empty: %v", err)
				}
			},
		},
		"browser sessions": {
			args:         []string{"--json", "browser", "sessions"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"browser-1"`, `"count": 1`},
		},
		"browser status": {
			args:         []string{"--json", "browser", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"available": true`, `"sessions": 1`},
		},
		"browser tabs": {
			args:         []string{"--json", "browser", "tabs", "browser-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"Example Domain"`, `"tab-1"`},
		},
		"bug-report": {
			argsFn: func(env *replayEnv) []string {
				path := filepath.Join(env.root, "bug-report.zip")
				env.values["bug_report"] = path
				return []string{"--json", "bug-report", "--include-logs=false", "--output", path}
			},
			configMode:   replayConfigMinimal,
			childEnv:     map[string]string{"OPENAI_API_KEY": ""},
			wantContains: []string{`"path":`, `bug-report.zip`},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if info, err := os.Stat(env.values["bug_report"]); err != nil || info.Size() == 0 {
					t.Fatalf("bug report missing or empty: %v", err)
				}
			},
		},
		"channels add": {
			args:         []string{"--json", "channels", "add", "discord", "--name", "discord-alerts", "--token", "discord-token"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"name": "discord-alerts"`},
		},
		"channels list": {
			args:         []string{"--json", "channels", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "slack"`, `"configured": true`},
		},
		"channels logs": {
			args:         []string{"--json", "channels", "logs", "slack", "--limit", "5"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"channel": "slack"`, `"channel.message.sent"`},
		},
		"channels remove": {
			args:         []string{"--json", "channels", "remove", "discord-alerts"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"name": "discord-alerts"`},
		},
		"channels status": {
			args:         []string{"--json", "channels", "status", "slack"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "slack"`, `"connected": true`},
		},
		"channels test": {
			args:         []string{"--json", "channels", "test", "slack", "--target", "room-1", "--message", "ping"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"message": "delivered"`},
		},
		"channels validate": {
			args:         []string{"--json", "channels", "validate", "slack"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"valid": true`, `"status": "connected"`},
		},
		"completion": {
			args:         []string{"completion", "bash"},
			configMode:   replayConfigDefault,
			wantContains: []string{"_hopclaw"},
		},
		"config edit": {
			args:       []string{"config", "edit"},
			configMode: replayConfigDefault,
			allowEmpty: true,
		},
		"config get": {
			args:         []string{"--json", "config", "get", "server.address"},
			configMode:   replayConfigDefault,
			wantContains: []string{"127.0.0.1"},
		},
		"config path": {
			args:       []string{"config", "path"},
			configMode: replayConfigDefault,
			verify: func(t *testing.T, env *replayEnv, output []byte) {
				if strings.TrimSpace(string(output)) != env.configPath {
					t.Fatalf("config path output = %q, want %q", strings.TrimSpace(string(output)), env.configPath)
				}
			},
		},
		"config set": {
			args:         []string{"config", "set", "runtime.profile", "production"},
			configMode:   replayConfigDefault,
			wantContains: []string{"set runtime.profile = production"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				data, err := os.ReadFile(env.configPath)
				if err != nil {
					t.Fatalf("ReadFile(config) error = %v", err)
				}
				if !strings.Contains(string(data), "profile: production") {
					t.Fatalf("config missing updated runtime.profile:\n%s", string(data))
				}
			},
		},
		"config show": {
			args:         []string{"--json", "config", "show"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"server":`, `"address":`},
		},
		"config unset": {
			args:         []string{"config", "unset", "runtime.profile"},
			configMode:   replayConfigDefault,
			wantContains: []string{"unset runtime.profile"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				data, err := os.ReadFile(env.configPath)
				if err != nil {
					t.Fatalf("ReadFile(config) error = %v", err)
				}
				if strings.Contains(string(data), "profile:") {
					t.Fatalf("config still contains runtime.profile:\n%s", string(data))
				}
			},
		},
		"config validate": {
			args:         []string{"--json", "config", "validate"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"server_address":`, `"default_model":`},
		},
		"dashboard": {
			args:         []string{"--json", "dashboard"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"url":`, `/dashboard/`},
		},
		"daemon install": {
			args:       []string{"daemon", "install"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
			},
			wantContains: []string{"service installed successfully", "binary:", "config:", "logs:"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				plist := launchdPlistPath(env)
				content, err := os.ReadFile(plist)
				if err != nil {
					t.Fatalf("ReadFile(%s) error = %v", plist, err)
				}
				text := string(content)
				if !strings.Contains(text, "<string>serve</string>") || !strings.Contains(text, env.configPath) {
					t.Fatalf("plist content = %q, want serve + config path", text)
				}
				if log := readTextFile(t, env.values["HOPCLAW_FAKE_LAUNCHCTL_LOG"]); !strings.Contains(log, "bootstrap gui/") {
					t.Fatalf("launchctl log = %q, want bootstrap call", log)
				}
			},
		},
		"daemon restart": {
			args:       []string{"daemon", "restart"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
				writeFakeLaunchdPlist(t, env)
				writeFakeLaunchctlState(t, env, true, true, 4242)
			},
			wantContains: []string{"service restarted"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				state := readFakeLaunchctlState(t, env)
				if state["bootstrapped"] != "1" || state["running"] != "1" {
					t.Fatalf("launchctl state = %#v, want running bootstrapped service", state)
				}
				log := readTextFile(t, env.values["HOPCLAW_FAKE_LAUNCHCTL_LOG"])
				if !strings.Contains(log, "bootout gui/") || !strings.Contains(log, "bootstrap gui/") {
					t.Fatalf("launchctl log = %q, want bootout then bootstrap", log)
				}
			},
		},
		"daemon start": {
			args:       []string{"daemon", "start"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
				writeFakeLaunchdPlist(t, env)
				writeFakeLaunchctlState(t, env, false, false, 0)
			},
			wantContains: []string{"service started"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				state := readFakeLaunchctlState(t, env)
				if state["bootstrapped"] != "1" || state["running"] != "1" || state["pid"] != "4242" {
					t.Fatalf("launchctl state = %#v, want bootstrapped running pid 4242", state)
				}
			},
		},
		"daemon status": {
			args:       []string{"--json", "daemon", "status"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
				writeFakeLaunchdPlist(t, env)
				writeFakeLaunchctlState(t, env, true, true, 4242)
			},
			wantContains: []string{`"installed": true`, `"running": true`, `"pid": 4242`, `"label": "com.hopclaw.gateway"`},
		},
		"daemon stop": {
			args:       []string{"daemon", "stop"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
				writeFakeLaunchdPlist(t, env)
				writeFakeLaunchctlState(t, env, true, true, 4242)
			},
			wantContains: []string{"service stopped"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				state := readFakeLaunchctlState(t, env)
				if state["bootstrapped"] != "0" || state["running"] != "0" {
					t.Fatalf("launchctl state = %#v, want stopped service", state)
				}
			},
		},
		"daemon uninstall": {
			args:       []string{"daemon", "uninstall"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
				writeFakeLaunchdPlist(t, env)
				writeFakeLaunchctlState(t, env, true, true, 4242)
			},
			wantContains: []string{"service uninstalled successfully"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if _, err := os.Stat(launchdPlistPath(env)); !os.IsNotExist(err) {
					t.Fatalf("plist still exists after uninstall, stat err = %v", err)
				}
				state := readFakeLaunchctlState(t, env)
				if state["bootstrapped"] != "0" {
					t.Fatalf("launchctl state = %#v, want bootstrapped=0 after uninstall", state)
				}
			},
		},
		"devices launch": {
			argsFn: func(env *replayEnv) []string {
				return []string{"devices", "launch", "desktopd", "--gateway-url", env.gatewayURL, "--pairing-code", "PAIR-DEV-123", "--device-id", "linux-test-device"}
			},
			configMode: replayConfigDefault,
			allowEmpty: true,
		},
		"devices pair": {
			args:         []string{"--json", "devices", "pair", "desktopd"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"daemon": "desktopd"`, `"pairing_code": "PAIR-DEV-123"`},
		},
		"dns discover": {
			args:         []string{"--json", "dns", "discover", "--timeout", "2"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"peer-a:16280"`, `"count": 1`},
		},
		"dns setup": {
			args:         []string{"--json", "dns", "setup", "--static", "peer-a:16280,peer-b:16280"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`},
		},
		"dns status": {
			args:         []string{"--json", "dns", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"tailscale": true`, `"static_peers"`},
		},
		"evals list": {
			args:         []string{"--json", "evals", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"smoke"`, `"case-1"`},
		},
		"evals run": {
			args:         []string{"--json", "evals", "run", "smoke"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "passed"`, `"case_count": 1`},
		},
		"doctor auth": {
			args:         []string{"--json", "doctor", "auth"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "auth"`, `"status":`},
		},
		"doctor config": {
			args:         []string{"--json", "doctor", "config"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "config"`, `"status":`},
		},
		"doctor connectivity": {
			args:         []string{"--json", "doctor", "connectivity"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "connectivity"`, `"status":`},
		},
		"doctor platform": {
			args:         []string{"--json", "doctor", "platform"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "platform"`, `"status":`},
		},
		"doctor security": {
			args:         []string{"--json", "doctor", "security"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "security"`, `"status":`},
		},
		"doctor skills": {
			args:         []string{"--json", "doctor", "skills"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "skills"`, `"status":`},
		},
		"doctor storage": {
			args:         []string{"--json", "doctor", "storage"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"category": "storage"`, `"status":`},
		},
		"health": {
			args:         []string{"--json", "health"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"healthy": true`},
		},
		"hooks delete": {
			args:         []string{"--json", "hooks", "delete", "hook-complete"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"deleted": "hook-complete"`},
		},
		"hooks errors": {
			args:         []string{"--json", "hooks", "errors", "hook-complete"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"timeout contacting webhook"`, `"count": 2`},
		},
		"hooks events": {
			args:         []string{"--json", "hooks", "events"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"trigger": "run.completed"`, `"category": "run"`},
		},
		"hooks inspect": {
			args:         []string{"--json", "hooks", "inspect", "hook-complete"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "hook-complete"`, `"name": "Completion webhook"`},
		},
		"hooks list": {
			args:         []string{"--json", "hooks", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"hook-complete"`, `"count": 1`},
		},
		"hooks recent": {
			args:         []string{"--json", "hooks", "recent", "hook-complete"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"hook_id": "hook-complete"`, `"status": "ok"`},
		},
		"hooks replay": {
			args:         []string{"--json", "hooks", "replay", "hook-complete"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"hook_id": "hook-complete"`, `"summary": "Replayed payload"`},
		},
		"hooks test-fire": {
			args:         []string{"--json", "hooks", "test-fire", "hook-complete", "--trigger", "run.completed", "--phase", "post", "--payload", `{"run_id":"run-1"}`},
			configMode:   replayConfigDefault,
			wantContains: []string{`"hook_id": "hook-complete"`, `"summary": "Hook delivered"`},
		},
		"logs list": {
			args:         []string{"--json", "logs", "list", "--limit", "5"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"evt-1"`, `"run.completed"`},
		},
		"logs stream": {
			args:         []string{"--json", "logs", "stream"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id":"evt-stream-1"`, `"type":"run.completed"`},
		},
		"memory delete": {
			args:         []string{"--json", "memory", "delete", "deploy_server", "--yes"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"deleted": "deploy_server"`},
		},
		"memory get": {
			args:         []string{"--json", "memory", "get", "deploy_server"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"key": "deploy_server"`, `"198.51.100.42"`},
		},
		"memory index": {
			args:         []string{"--json", "memory", "index", "--force"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "ok"`, `"indexed": 2`},
		},
		"memory list": {
			args:         []string{"--json", "memory", "list", "--limit", "10"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"count": 2`, `"deploy_server"`},
		},
		"memory search": {
			args:         []string{"--json", "memory", "search", "deploy"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"deploy_server"`},
		},
		"memory set": {
			args:         []string{"--json", "memory", "set", "deploy_server", "198.51.100.42"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "ok"`, `"key": "deploy_server"`},
		},
		"memory status": {
			args:         []string{"--json", "memory", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"store_type": "in-memory"`, `"entry_count": 2`},
		},
		"message broadcast": {
			args:         []string{"--json", "message", "broadcast", "--channels", "slack,discord", "--content", "heads up"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"channel": "slack"`, `"run_id": "run-broadcast-slack"`},
		},
		"message delete": {
			args:         []string{"--json", "message", "delete", "run-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"deleted": "run-1"`},
		},
		"message edit": {
			args:         []string{"--json", "message", "edit", "run-1", "--content", "updated body"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "run-1"`, `"status": "completed"`},
		},
		"message list": {
			args:         []string{"--json", "message", "list", "--limit", "5"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "run-1"`, `"count": 1`},
		},
		"message react": {
			args:         []string{"--json", "message", "react", "run-1", "--emoji", "rocket"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"run_id": "run-1"`, `"emoji": "rocket"`},
		},
		"message read": {
			args:         []string{"--json", "message", "read", "sess-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "sess-1"`, `"messages"`},
		},
		"message search": {
			args:         []string{"--json", "message", "search", "deploy", "--limit", "5"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"content": "deploy status"`},
		},
		"message send": {
			args:         []string{"--json", "message", "send", "hello"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"final_text": "Hello from fake run"`},
		},
		"message thread": {
			args:         []string{"--json", "message", "thread", "sess-1", "--content", "reply here"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "run-thread-1"`, `"session_id": "sess-1"`},
		},
		"models add": {
			args:         []string{"--json", "models", "add", "replay-openai", "--api", "openai-completions", "--set", "api_key=sk-test", "--set", "base_url=https://api.example.com/v1", "--set", "default_model=gpt-5.4"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"name": "replay-openai"`},
		},
		"models bench": {
			args:         []string{"--json", "models", "bench"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"provider": "custom-openai"`, `"model": "gpt-5.4"`},
		},
		"models delete": {
			args:         []string{"--json", "models", "delete", "custom-openai"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"name": "custom-openai"`},
		},
		"models info": {
			args:         []string{"--json", "models", "info", "custom-openai"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"provider":`, `"custom-openai"`},
		},
		"models list": {
			args:         []string{"--json", "models", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"provider": "custom-openai"`, `"auth_configured": true`},
		},
		"models router": {
			args:         []string{"--json", "models", "router"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "custom-openai/default"`, `"provider": "custom-openai"`},
		},
		"models status": {
			args:         []string{"--json", "models", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"version": "test-v1"`, `"capability_count": 6`},
		},
		"models test": {
			args:         []string{"--json", "models", "test", "custom-openai"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"provider": "custom-openai"`, `"status": "ok"`},
		},
		"models test-chat": {
			args:         []string{"--json", "models", "test-chat", "custom-openai", "--message", "hello"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"reply": "Hello from fake model"`},
		},
		"models update": {
			args:         []string{"--json", "models", "update", "custom-openai", "--set", "default_model=gpt-5.5"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"name": "custom-openai"`},
		},
		"models validate": {
			args:         []string{"--json", "models", "validate", "custom-openai"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"valid": true`, `"model": "gpt-5.4"`},
		},
		"nodes list": {
			args:         []string{"--json", "nodes", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"count": 1`, `"address"`},
		},
		"nodes ping": {
			argsFn:       func(env *replayEnv) []string { return []string{"--json", "nodes", "ping", env.gatewayHost} },
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "ok"`, `"latency":`},
		},
		"nodes status": {
			argsFn:       func(env *replayEnv) []string { return []string{"--json", "nodes", "status", env.gatewayHost} },
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "ok"`, `"version": "test-v1"`},
		},
		"onboard": {
			args:       []string{"onboard", "--non-interactive"},
			configMode: replayConfigNone,
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if _, err := os.Stat(filepath.Join(env.stateDir, "config.yaml")); err != nil {
					t.Fatalf("onboard did not create config.yaml: %v", err)
				}
			},
		},
		"pairing initiate": {
			args:         []string{"--json", "pairing", "initiate", "slack", "u-1", "--name", "Demo User"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"channel": "slack"`, `"code": "PAIR-123"`},
		},
		"pairing list": {
			args:         []string{"--json", "pairing", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"channel": "slack"`, `"count": 1`},
		},
		"pairing revoke": {
			args:         []string{"--json", "pairing", "revoke", "slack", "u-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`},
		},
		"pairing verify": {
			args:         []string{"--json", "pairing", "verify", "PAIR-123"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": "verified"`},
		},
		"plugins disable": {
			args:         []string{"--json", "plugins", "disable", "example-plugin"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "example-plugin"`, `"ok": true`},
		},
		"plugins enable": {
			args:         []string{"--json", "plugins", "enable", "example-plugin"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "example-plugin"`, `"ok": true`},
		},
		"plugins info": {
			args:         []string{"--json", "plugins", "info", "example-plugin"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"plugin":`, `"example-plugin"`},
		},
		"plugins init": {
			argsFn: func(env *replayEnv) []string {
				return []string{"plugins", "init", "sample-plugin", "--dir", env.workdir}
			},
			configMode:   replayConfigDefault,
			wantContains: []string{"Initialized tool plugin"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if _, err := os.Stat(filepath.Join(env.workdir, "sample-plugin", "hopclaw.plugin.yaml")); err != nil {
					t.Fatalf("scaffold manifest missing: %v", err)
				}
			},
		},
		"plugins install": {
			args:         []string{"--json", "plugins", "install", "example-plugin"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "example-plugin"`, `"ok": true`},
		},
		"plugins list": {
			args:         []string{"--json", "plugins", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"example-plugin"`, `"count": 1`},
		},
		"plugins uninstall": {
			args:         []string{"--json", "plugins", "uninstall", "example-plugin"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "example-plugin"`, `"ok": true`},
		},
		"plugins validate": {
			argsFn: func(env *replayEnv) []string {
				return []string{"--json", "plugins", "validate", env.values["plugin_dir"]}
			},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				cmd := &cobra.Command{}
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				if err := runPluginsInit(cmd, "validate-plugin", string(pluginScaffoldTool), env.workdir); err != nil {
					t.Fatalf("runPluginsInit() error = %v", err)
				}
				env.values["plugin_dir"] = filepath.Join(env.workdir, "validate-plugin")
			},
			wantContains: []string{`"ok": true`, `"validate-plugin"`},
		},
		"project delete": {
			args:         []string{"--json", "project", "delete", "demo", "--yes"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"deleted": "demo"`},
		},
		"project list": {
			args:         []string{"--json", "project", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "demo"`},
		},
		"project rename": {
			args:         []string{"--json", "project", "rename", "demo", "demo-renamed"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"old_name": "demo"`, `"name": "demo-renamed"`},
		},
		"project show": {
			args:         []string{"--json", "project", "show", "demo"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"directory": "/tmp/demo-project"`},
		},
		"qr generate": {
			args:         []string{"--json", "qr", "generate", "--session", "cli-session", "--channel", "webchat"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"session": "cli-session"`, `/dashboard?channel=webchat\u0026session=cli-session`},
		},
		"qr show": {
			args:         []string{"--json", "qr", "show"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"url":`, envURLPlaceholder()},
		},
		"quality readiness": {
			args:         []string{"--json", "quality", "readiness"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ready": true`, `"sample_size"`},
		},
		"quality summary": {
			args:         []string{"--json", "quality", "summary"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"run_count": 12`, `"task_success"`},
		},
		"remote add": {
			args:         []string{"--json", "remote", "add", "prod", "https://prod.example.com", "--token-env", "REMOTE_TOKEN"},
			configMode:   replayConfigDefault,
			childEnv:     map[string]string{"REMOTE_TOKEN": "secret-token"},
			wantContains: []string{`"name": "prod"`, `"base_url": "https://prod.example.com"`},
		},
		"remote get": {
			args:       []string{"--json", "remote", "get", "prod"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: "https://prod.example.com", AuthType: targetAuthTypeBearer, AuthRef: "env:REMOTE_TOKEN"}); err != nil {
					t.Fatalf("addSavedTargetProfile() error = %v", err)
				}
			},
			childEnv:     map[string]string{"REMOTE_TOKEN": "secret-token"},
			wantContains: []string{`"auth_configured": true`, `"name": "prod"`},
		},
		"remote list": {
			args:       []string{"--json", "remote", "list"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: "https://prod.example.com"}); err != nil {
					t.Fatalf("addSavedTargetProfile() error = %v", err)
				}
			},
			wantContains: []string{`"name": "prod"`, `"name": "local"`},
		},
		"remote login": {
			args:       []string{"--json", "remote", "login", "prod", "--token-env", "REMOTE_TOKEN"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: "https://prod.example.com"}); err != nil {
					t.Fatalf("addSavedTargetProfile() error = %v", err)
				}
			},
			childEnv:     map[string]string{"REMOTE_TOKEN": "secret-token"},
			wantContains: []string{`"auth_type": "bearer"`, `"name": "prod"`},
		},
		"remote logout": {
			args:       []string{"--json", "remote", "logout", "prod"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: "https://prod.example.com", AuthType: targetAuthTypeBearer, AuthRef: "env:REMOTE_TOKEN"}); err != nil {
					t.Fatalf("addSavedTargetProfile() error = %v", err)
				}
			},
			wantContains: []string{`"auth_type": "none"`, `"name": "prod"`},
		},
		"remote remove": {
			args:       []string{"--json", "remote", "remove", "prod"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				if err := addSavedTargetProfile(savedTargetProfile{Name: "prod", Kind: targetKindRemote, BaseURL: "https://prod.example.com"}); err != nil {
					t.Fatalf("addSavedTargetProfile() error = %v", err)
				}
			},
			wantContains: []string{`"deleted": "prod"`},
		},
		"remote test": {
			args:         []string{"--json", "remote", "test", "local"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok": true`, `"kind": "local"`},
		},
		"sandbox images": {
			args:         []string{"--json", "sandbox", "images"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"python:3.12-slim"`},
		},
		"sandbox run": {
			args:         []string{"--json", "sandbox", "run", "--image", "python:3.12-slim", "--", "echo", "hello"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"stdout": "hello\n"`, `"exit_code": 0`},
		},
		"sandbox status": {
			args:         []string{"--json", "sandbox", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"available": true`, `"runtime": "docker"`},
		},
		"security audit": {
			args:         []string{"--json", "security", "audit"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"results":`, `"summary":`},
		},
		"security rotate": {
			args:       []string{"--json", "security", "rotate", "--auth-token"},
			configMode: replayConfigDefault,
			prepare: func(t *testing.T, env *replayEnv) {
				writeReplayConfig(t, env.configPath, env.gatewayHost, replayConfigDefault, true)
			},
			wantContains: []string{`"token":`},
		},
		"sessions export": {
			args:         []string{"sessions", "export", "sess-1", "--format", "json"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"role":"user"`, `"role":"assistant"`},
		},
		"sessions get": {
			args:         []string{"--json", "sessions", "get", "sess-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "sess-1"`, `"messages"`},
		},
		"sessions list": {
			args:         []string{"--json", "sessions", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"sess-1"`, `"count": 2`},
		},
		"sessions prune": {
			args:         []string{"sessions", "prune", "--older-than", "30d", "--dry-run"},
			configMode:   replayConfigDefault,
			wantContains: []string{"Would prune 1 conversations"},
		},
		"secrets delete": {
			args:       []string{"secrets", "delete", "openai-api-key"},
			configMode: replayConfigNone,
			prepare: func(t *testing.T, env *replayEnv) {
				seedFileBackedKeychain(t, env, map[string]string{"openai-api-key": "sk-cli-123"})
			},
			wantContains: []string{`deleted "openai-api-key" from keychain`},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				secrets := readFileBackedKeychain(t, env)
				if _, ok := secrets["openai-api-key"]; ok {
					t.Fatalf("file-backed keychain still contains deleted key: %#v", secrets)
				}
			},
		},
		"secrets get": {
			args:       []string{"secrets", "get", "openai-api-key"},
			configMode: replayConfigNone,
			prepare: func(t *testing.T, env *replayEnv) {
				seedFileBackedKeychain(t, env, map[string]string{"openai-api-key": "sk-cli-123"})
			},
			wantContains: []string{"sk-cli-123"},
		},
		"secrets list": {
			args:       []string{"--json", "secrets", "list"},
			configMode: replayConfigNone,
			prepare: func(t *testing.T, env *replayEnv) {
				seedFileBackedKeychain(t, env, map[string]string{"openai-api-key": "sk-cli-123"})
			},
			wantContains: []string{`"key": "openai-api-key"`, `"status": "stored"`},
		},
		"secrets set": {
			args:       []string{"secrets", "set", "openai-api-key", "sk-cli-123"},
			configMode: replayConfigNone,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFileBackedKeychain(env)
			},
			wantContains: []string{`stored "openai-api-key" in keychain`},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				secrets := readFileBackedKeychain(t, env)
				if secrets["openai-api-key"] != "sk-cli-123" {
					t.Fatalf("file-backed keychain = %#v, want openai-api-key", secrets)
				}
			},
		},
		"serve": {
			args:       []string{"serve", "--name", "local-dev"},
			configMode: replayConfigNone,
			allowEmpty: true,
			prepare: func(t *testing.T, env *replayEnv) {
				address := allocateReplayAddress(t)
				env.values["HOPCLAW_SERVE_ADDRESS"] = address
				writeServeReplayConfig(t, env.configPath, address)
			},
			run:          runServeReplay,
			wantContains: []string{"hopclaw listening"},
		},
		"setup": {
			args:         []string{"setup", "--non-interactive"},
			configMode:   replayConfigNone,
			wantContains: []string{"Config written to"},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if _, err := os.Stat(filepath.Join(env.stateDir, "config.yaml")); err != nil {
					t.Fatalf("setup did not create config.yaml: %v", err)
				}
			},
		},
		"skills info": {
			args:         []string{"--json", "skills", "info", "summarize"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"installed":`, `"catalog":`},
		},
		"skills install": {
			args:         []string{"--json", "skills", "install", "summarize"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"skill_id": "summarize"`},
		},
		"skills list": {
			args:         []string{"--json", "skills", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "Summarize"`, `"count": 1`},
		},
		"skills remove": {
			args:         []string{"--json", "skills", "remove", "summarize"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"deleted": "summarize"`},
		},
		"skills search": {
			args:         []string{"--json", "skills", "search", "sum"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"summarize"`},
		},
		"status": {
			args:         []string{"--json", "status"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"ok":true`, `"version":"test-v1"`},
		},
		"tools check": {
			args:         []string{"--json", "tools", "check", "shell"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "shell"`, `"eligible": true`},
		},
		"tools info": {
			args:         []string{"--json", "tools", "info", "shell"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "shell"`, `"source": "builtin"`},
		},
		"tools list": {
			args:         []string{"--json", "tools", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"name": "shell"`},
		},
		"tools search": {
			args:         []string{"--json", "tools", "search", "shell"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"shell"`},
		},
		"uninstall": {
			args:       []string{"uninstall", "--yes"},
			configMode: replayConfigNone,
			prepare: func(t *testing.T, env *replayEnv) {
				enableFakeLaunchctl(t, env)
				writeFakeLaunchdPlist(t, env)
				writeFakeLaunchctlState(t, env, true, true, 4242)
				if err := os.WriteFile(filepath.Join(env.stateDir, "config.yaml"), []byte("server:\n  address: \"127.0.0.1:16280\"\n"), 0o644); err != nil {
					t.Fatalf("WriteFile(config.yaml) error = %v", err)
				}
				cacheDir := filepath.Join(env.home, "Library", "Caches", "hopclaw")
				if err := os.MkdirAll(cacheDir, 0o755); err != nil {
					t.Fatalf("MkdirAll(cacheDir) error = %v", err)
				}
				if err := os.WriteFile(filepath.Join(cacheDir, "cache.txt"), []byte("cached"), 0o644); err != nil {
					t.Fatalf("WriteFile(cache.txt) error = %v", err)
				}
			},
			run:          runCopiedBinaryReplay,
			wantContains: []string{"HopClaw has been uninstalled."},
			verify: func(t *testing.T, env *replayEnv, _ []byte) {
				if _, err := os.Stat(env.stateDir); !os.IsNotExist(err) {
					t.Fatalf("state dir still exists after uninstall, stat err=%v", err)
				}
				if _, err := os.Stat(env.values["HOPCLAW_REPLAY_BINARY_COPY"]); !os.IsNotExist(err) {
					t.Fatalf("binary copy still exists after uninstall, stat err=%v", err)
				}
				log := readTextFile(t, env.values["HOPCLAW_FAKE_LAUNCHCTL_LOG"])
				if !strings.Contains(log, "print gui/") || !strings.Contains(log, "bootout gui/") {
					t.Fatalf("launchctl log = %q, want status + bootout calls", log)
				}
			},
		},
		"update": {
			args:       []string{"--json", "update", "--check"},
			configMode: replayConfigNone,
			prepare: func(t *testing.T, env *replayEnv) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writeJSONResponse(t, w, http.StatusOK, []map[string]any{{
						"tag_name":     "2026.04.07",
						"html_url":     "https://example.com/hopclaw/releases/2026.04.07",
						"body":         "Bug fixes and stability improvements.",
						"prerelease":   false,
						"published_at": "2026-04-07T12:00:00Z",
						"assets": []map[string]any{{
							"name":                 "hopclaw-darwin-arm64.tar.gz",
							"browser_download_url": "https://example.com/hopclaw/releases/download/2026.04.07/hopclaw-darwin-arm64.tar.gz",
						}},
					}})
				}))
				t.Cleanup(server.Close)
				env.values["HOPCLAW_UPDATE_API_URL"] = server.URL + "/repos/fulcrus/hopclaw/releases"
			},
			wantContains: []string{`"current_version": "2026.04.06"`, `"latest_version":`, `"checked_at":`},
			verify: func(t *testing.T, _ *replayEnv, output []byte) {
				var payload map[string]any
				if err := json.Unmarshal(output, &payload); err != nil {
					t.Fatalf("decode update output: %v", err)
				}
				if strings.TrimSpace(fmt.Sprint(payload["latest_version"])) == "" {
					t.Fatalf("update payload missing latest_version: %s", string(output))
				}
				if _, ok := payload["up_to_date"]; !ok {
					t.Fatalf("update payload missing up_to_date: %s", string(output))
				}
			},
		},
		"version": {
			args:         []string{"version"},
			configMode:   replayConfigDefault,
			wantContains: []string{"hopclaw "},
		},
		"webhooks create": {
			args:         []string{"--json", "webhooks", "create", "--url", "https://example.com/hook", "--events", "run.completed"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "wh-1"`},
		},
		"webhooks delete": {
			args:         []string{"--json", "webhooks", "delete", "wh-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"id": "wh-1"`, `"ok": true`},
		},
		"webhooks info": {
			args:         []string{"--json", "webhooks", "info", "wh-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"webhook":`, `"recent_deliveries"`},
		},
		"webhooks list": {
			args:         []string{"--json", "webhooks", "list"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"wh-1"`, `"count": 1`},
		},
		"webhooks test": {
			args:         []string{"--json", "webhooks", "test", "wh-1"},
			configMode:   replayConfigDefault,
			wantContains: []string{`"status": 200`, `"ok": true`},
		},
	}
}

func newReplayEnv(t *testing.T, gatewayURL string, mode replayConfigMode) *replayEnv {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workdir := filepath.Join(root, "work")
	binDir := filepath.Join(root, "bin")
	stateDir := filepath.Join(home, ".hopclaw")
	configPath := filepath.Join(stateDir, "config.yaml")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll(home) error = %v", err)
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workdir) error = %v", err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(binDir) error = %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(stateDir) error = %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("OPENAI_API_KEY", "sk-test-123456")

	editorPath := filepath.Join(binDir, "fake-editor")
	if err := os.WriteFile(editorPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(fake-editor) error = %v", err)
	}
	for _, daemonName := range []string{"hopclaw-browserd", "hopclaw-desktopd"} {
		path := filepath.Join(binDir, daemonName)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", daemonName, err)
		}
	}

	host := strings.TrimPrefix(gatewayURL, "http://")
	if mode != replayConfigNone {
		writeReplayConfig(t, configPath, host, mode, false)
	}

	return &replayEnv{
		root:        root,
		home:        home,
		workdir:     workdir,
		binDir:      binDir,
		configPath:  configPath,
		stateDir:    stateDir,
		editorPath:  editorPath,
		gatewayURL:  gatewayURL,
		gatewayHost: host,
		values:      map[string]string{},
	}
}

func replayArgs(plan *replayCase, env *replayEnv) []string {
	args := append([]string(nil), plan.args...)
	if plan.argsFn != nil {
		args = plan.argsFn(env)
	}
	return args
}

func runReplayCase(t *testing.T, bin string, env *replayEnv, plan *replayCase) ([]byte, int) {
	t.Helper()
	if plan.run != nil {
		return plan.run(t, bin, env, plan)
	}
	return runReplayBinary(t, bin, env, plan)
}

func runReplayBinary(t *testing.T, binary string, env *replayEnv, plan *replayCase) ([]byte, int) {
	t.Helper()
	cmd := exec.Command(binary, replayArgs(plan, env)...)
	cmd.Dir = env.workdir
	if plan.stdin != "" {
		cmd.Stdin = strings.NewReader(plan.stdin)
	}
	childEnv := replayChildEnv(env)
	for key, value := range plan.childEnv {
		childEnv = append(childEnv, key+"="+value)
	}
	cmd.Env = childEnv

	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errorsAs(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("command %q failed to start: %v\noutput=%s", strings.Join(replayArgs(plan, env), " "), err, string(output))
		}
	}
	return output, exitCode
}

func replayChildEnv(env *replayEnv) []string {
	entries := []string{
		"HOME=" + env.home,
		"PATH=" + env.binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"EDITOR=" + env.editorPath,
		"OPENAI_API_KEY=sk-test-123456",
		// Go telemetry writes asynchronously to HOME/Library/Application
		// Support/go/telemetry on darwin and similar paths elsewhere. With
		// HOME redirected to t.TempDir(), the writer can still be flushing
		// when the test finishes and t.TempDir's RemoveAll fires, which
		// surfaces as a "directory not empty" cleanup error. Disable it.
		"GOTELEMETRY=off",
		"GODEBUG=gotelemetry=off",
	}
	if len(env.values) > 0 {
		keys := make([]string, 0, len(env.values))
		for key := range env.values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			entries = append(entries, key+"="+env.values[key])
		}
	}
	if env.configPath != "" {
		entries = append(entries, "HOPCLAW_CONFIG="+env.configPath)
	}
	return entries
}

func enableFakeLaunchctl(t *testing.T, env *replayEnv) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("daemon replay relies on a fake launchctl shim and only runs on darwin; the production daemon path on linux uses systemctl/dbus, which is not available in the GitHub Actions ubuntu runner")
	}
	env.values["HOPCLAW_FAKE_LAUNCHCTL_STATE"] = filepath.Join(env.root, "launchctl.state")
	env.values["HOPCLAW_FAKE_LAUNCHCTL_LOG"] = filepath.Join(env.root, "launchctl.log")

	scriptPath := filepath.Join(env.binDir, "launchctl")
	script := `#!/bin/sh
set -eu
state="${HOPCLAW_FAKE_LAUNCHCTL_STATE:?}"
log="${HOPCLAW_FAKE_LAUNCHCTL_LOG:?}"
cmd="${1:-}"
if [ "$#" -gt 0 ]; then
  shift
fi
mkdir -p "$(dirname "$state")"
touch "$log"
read_state() {
  bootstrapped=0
  running=0
  pid=0
  if [ -f "$state" ]; then
    while IFS='=' read -r key value; do
      case "$key" in
        bootstrapped) bootstrapped="$value" ;;
        running) running="$value" ;;
        pid) pid="$value" ;;
      esac
    done < "$state"
  fi
}
write_state() {
  cat >"$state" <<EOF
bootstrapped=$bootstrapped
running=$running
pid=$pid
EOF
}
read_state
printf '%s %s\n' "$cmd" "$*" >> "$log"
case "$cmd" in
  bootstrap)
    if [ "$bootstrapped" = "1" ]; then
      echo "already bootstrapped"
      exit 1
    fi
    bootstrapped=1
    running=1
    if [ "$pid" = "0" ]; then
      pid=4242
    fi
    write_state
    exit 0
    ;;
  bootout)
    if [ "$bootstrapped" != "1" ]; then
      echo "No such process"
      exit 1
    fi
    bootstrapped=0
    running=0
    pid=0
    write_state
    exit 0
    ;;
  kickstart)
    bootstrapped=1
    running=1
    if [ "$pid" = "0" ]; then
      pid=4242
    fi
    write_state
    exit 0
    ;;
  print)
    if [ "$bootstrapped" != "1" ]; then
      echo "not found"
      exit 1
    fi
    if [ "$running" = "1" ]; then
      echo "state = running"
    else
      echo "state = not running"
    fi
    echo "pid = $pid"
    exit 0
    ;;
  *)
    echo "unsupported $cmd"
    exit 1
    ;;
esac
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake launchctl) error = %v", err)
	}
}

func launchdPlistPath(env *replayEnv) string {
	return filepath.Join(env.home, "Library", "LaunchAgents", "com.hopclaw.gateway.plist")
}

func writeFakeLaunchdPlist(t *testing.T, env *replayEnv) {
	t.Helper()
	path := launchdPlistPath(env)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(launchd plist dir) error = %v", err)
	}
	if err := os.WriteFile(path, []byte("<plist></plist>\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(launchd plist) error = %v", err)
	}
}

func writeFakeLaunchctlState(t *testing.T, env *replayEnv, bootstrapped, running bool, pid int) {
	t.Helper()
	statePath := env.values["HOPCLAW_FAKE_LAUNCHCTL_STATE"]
	if statePath == "" {
		t.Fatal("fake launchctl state path not configured")
	}
	content := fmt.Sprintf("bootstrapped=%d\nrunning=%d\npid=%d\n", boolToInt(bootstrapped), boolToInt(running), pid)
	if err := os.WriteFile(statePath, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(fake launchctl state) error = %v", err)
	}
}

func readFakeLaunchctlState(t *testing.T, env *replayEnv) map[string]string {
	t.Helper()
	statePath := env.values["HOPCLAW_FAKE_LAUNCHCTL_STATE"]
	if statePath == "" {
		t.Fatal("fake launchctl state path not configured")
	}
	return readKeyValueFile(t, statePath)
}

func readTextFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func readKeyValueFile(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("invalid key=value line %q in %s", line, path)
		}
		values[key] = value
	}
	return values
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func enableFileBackedKeychain(env *replayEnv) {
	env.values["HOPCLAW_KEYCHAIN_FILE"] = filepath.Join(env.stateDir, "keychain.json")
}

func seedFileBackedKeychain(t *testing.T, env *replayEnv, secrets map[string]string) {
	t.Helper()
	enableFileBackedKeychain(env)
	payload := map[string]map[string]string{
		"hopclaw": secrets,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent(file-backed keychain) error = %v", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(env.values["HOPCLAW_KEYCHAIN_FILE"], data, 0o600); err != nil {
		t.Fatalf("WriteFile(file-backed keychain) error = %v", err)
	}
}

func readFileBackedKeychain(t *testing.T, env *replayEnv) map[string]string {
	t.Helper()
	payload := readJSONFileMap(t, env.values["HOPCLAW_KEYCHAIN_FILE"])
	service, _ := payload["hopclaw"].(map[string]any)
	secrets := map[string]string{}
	for key, value := range service {
		secrets[key] = fmt.Sprint(value)
	}
	return secrets
}

func readJSONFileMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal(%s) error = %v", path, err)
	}
	return payload
}

func allocateReplayAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()
	return listener.Addr().String()
}

func writeServeReplayConfig(t *testing.T, path, address string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}
	content := fmt.Sprintf(`# HopClaw replay config
server:
  address: "%s"
store:
  backend: memory
agent:
  default_model: "unconfigured-model"
tools:
  builtins:
    enabled: true
update:
  enabled: false
  check_on_start: false
diagnostics:
  telemetry_enabled: false
  telemetry_collector_enabled: false
`, address)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(serve replay config) error = %v", err)
	}
}

func runServeReplay(t *testing.T, bin string, env *replayEnv, plan *replayCase) ([]byte, int) {
	t.Helper()
	args := replayArgs(plan, env)
	cmd := exec.Command(bin, args...)
	cmd.Dir = env.workdir
	childEnv := replayChildEnv(env)
	for key, value := range plan.childEnv {
		childEnv = append(childEnv, key+"="+value)
	}
	cmd.Env = childEnv

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		t.Fatalf("command %q failed to start: %v", strings.Join(args, " "), err)
	}

	address := env.values["HOPCLAW_SERVE_ADDRESS"]
	if address == "" {
		t.Fatal("HOPCLAW_SERVE_ADDRESS not configured")
	}
	baseURL := "http://" + address
	waitForHTTP(t, baseURL+"/healthz", 10*time.Second)
	assertServeInstanceRecord(t, env, baseURL, "local-dev")

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("interrupt serve process: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		exitCode := 0
		if err != nil {
			var exitErr *exec.ExitError
			if errorsAs(err, &exitErr) {
				exitCode = exitErr.ExitCode()
			} else {
				t.Fatalf("wait serve process: %v\noutput=%s", err, output.String())
			}
		}
		assertServeInstanceShutdown(t, env)
		return output.Bytes(), exitCode
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("serve process did not exit after interrupt\noutput=%s", output.String())
		return nil, 1
	}
}

func runCopiedBinaryReplay(t *testing.T, bin string, env *replayEnv, plan *replayCase) ([]byte, int) {
	t.Helper()
	copyPath := filepath.Join(env.root, "replay-bin", "hopclaw")
	if err := os.MkdirAll(filepath.Dir(copyPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(replay-bin) error = %v", err)
	}
	copyExecutable(t, bin, copyPath)
	env.values["HOPCLAW_REPLAY_BINARY_COPY"] = copyPath
	return runReplayBinary(t, copyPath, env, plan)
}

func copyExecutable(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("Open(%s) error = %v", src, err)
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		t.Fatalf("Stat(%s) error = %v", src, err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		t.Fatalf("OpenFile(%s) error = %v", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("Copy(%s -> %s) error = %v", src, dst, err)
	}
}

func waitForHTTP(t *testing.T, rawURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(rawURL)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s: %v", rawURL, lastErr)
}

func assertServeInstanceRecord(t *testing.T, env *replayEnv, baseURL, wantName string) {
	t.Helper()
	instanceDir := filepath.Join(env.stateDir, "instances")
	entries, err := os.ReadDir(instanceDir)
	if err != nil {
		t.Fatalf("ReadDir(%s) error = %v", instanceDir, err)
	}
	if len(entries) != 1 {
		t.Fatalf("serve instance entries = %d, want 1", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(instanceDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile(instance record) error = %v", err)
	}
	var record struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		PID     int    `json:"pid"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("Unmarshal(instance record) error = %v", err)
	}
	if record.Name != wantName {
		t.Fatalf("instance name = %q, want %q", record.Name, wantName)
	}
	if record.BaseURL != baseURL {
		t.Fatalf("instance base_url = %q, want %q", record.BaseURL, baseURL)
	}
	if record.PID <= 0 {
		t.Fatalf("instance pid = %d, want > 0", record.PID)
	}
}

func assertServeInstanceShutdown(t *testing.T, env *replayEnv) {
	t.Helper()
	instanceDir := filepath.Join(env.stateDir, "instances")
	entries, err := os.ReadDir(instanceDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir(%s) error = %v", instanceDir, err)
	}
	if len(entries) != 0 {
		t.Fatalf("serve instance entries after shutdown = %d, want 0", len(entries))
	}
}

func writeReplayConfig(t *testing.T, path, gatewayHost string, mode replayConfigMode, withAuthToken bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(config dir) error = %v", err)
	}
	var content string
	switch mode {
	case replayConfigMinimal:
		content = fmt.Sprintf("server:\n  address: \"%s\"\n", gatewayHost)
		if withAuthToken {
			content += "  auth_token: \"old-token\"\n"
		}
		content += "store:\n  backend: memory\nagent:\n  default_model: \"unconfigured-model\"\ntools:\n  builtins:\n    enabled: true\n"
	default:
		content = config.GenerateDefaultConfig()
		content = strings.Replace(content, config.DefaultGatewayAddress, gatewayHost, 1)
		if withAuthToken {
			needle := fmt.Sprintf("server:\n  address: \"%s\"\n", gatewayHost)
			replacement := needle + "  auth_token: \"old-token\"\n"
			if strings.Contains(content, needle) {
				content = strings.Replace(content, needle, replacement, 1)
			} else if strings.Contains(content, "server:\n") {
				content = strings.Replace(content, "server:\n", "server:\n  auth_token: \"old-token\"\n", 1)
			}
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(config) error = %v", err)
	}
}

func lookupCommand(root *cobra.Command, path []string) *cobra.Command {
	current := root
	for _, name := range path {
		var next *cobra.Command
		for _, child := range current.Commands() {
			if child.Hidden || child.Name() != name {
				continue
			}
			next = child
			break
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

func newCLIReplayGateway(t *testing.T) *httptest.Server {
	t.Helper()
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	ts := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case r.URL.Path == "/operator/status":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ok":               true,
				"status":           "ok",
				"version":          "test-v1",
				"uptime":           "1h",
				"capability_count": 6,
				"channels":         []map[string]any{{"name": "slack", "status": "connected"}},
			})
		case r.URL.Path == setupCatalogPath:
			writeJSONResponse(t, w, http.StatusOK, config.CurrentOperatorSetupCatalog())
		case r.URL.Path == "/runtime/quality/summary":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"run_count":            12,
				"terminal_run_count":   10,
				"task_success":         map[string]any{"count": 8, "total": 10, "rate": 0.8},
				"false_success":        map[string]any{"count": 0, "total": 10, "rate": 0.0},
				"verification_failure": map[string]any{"count": 1, "total": 10, "rate": 0.1},
				"trace_count":          10,
			})
		case r.URL.Path == "/runtime/release-readiness":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"ready":    true,
				"checks":   []map[string]any{{"id": "sample_size", "status": "passed", "summary": "enough terminal runs"}},
				"blockers": []any{},
			})
		case r.URL.Path == "/runtime/evals/suites":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{{
					"id":      "smoke",
					"name":    "Smoke Suite",
					"surface": "cli",
					"cases":   []map[string]any{{"id": "case-1", "name": "hello", "prompt": "Hello"}},
				}},
				"count": 1,
			})
		case r.URL.Path == "/runtime/evals/run":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"suite":      map[string]any{"id": "smoke"},
				"status":     "passed",
				"case_count": 1,
				"passed":     1,
				"failed":     0,
				"errored":    0,
			})
		case r.URL.Path == "/runtime/runs" && r.Method == http.MethodGet:
			if strings.TrimSpace(r.URL.Query().Get("q")) != "" {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{
					"items": []map[string]any{{
						"id":         "run-1",
						"session_id": "sess-1",
						"status":     "completed",
						"content":    "deploy status",
					}},
					"count": 1,
				})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{{
					"id":         "run-1",
					"session_id": "sess-1",
					"status":     "completed",
					"phase":      "responded",
					"model":      "gpt-5.4",
					"created_at": ts.Format(time.RFC3339),
				}},
				"count": 1,
			})
		case r.URL.Path == "/runtime/runs" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			sessionKey := strings.TrimSpace(fmt.Sprint(body["session_key"]))
			threadID := strings.TrimSpace(fmt.Sprint(body["thread_id"]))
			runID := "run-send-1"
			sessionID := "sess-1"
			switch {
			case threadID != "" && threadID != "<nil>":
				runID = "run-thread-1"
			case strings.HasPrefix(sessionKey, "slack:"):
				runID = "run-broadcast-slack"
				sessionID = "sess-broadcast"
			case strings.HasPrefix(sessionKey, "discord:"):
				runID = "run-broadcast-discord"
				sessionID = "sess-broadcast"
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":         runID,
				"session_id": sessionID,
				"status":     "completed",
				"phase":      "responded",
				"model":      "gpt-5.4",
				"created_at": ts.Format(time.RFC3339),
			})
		case strings.HasPrefix(r.URL.Path, "/runtime/runs/") && strings.HasSuffix(r.URL.Path, "/completion"):
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"bundle": map[string]any{
					"final_text": "Hello from fake run",
				},
			})
		case strings.HasPrefix(r.URL.Path, "/runtime/runs/") && strings.HasSuffix(r.URL.Path, "/react") && r.Method == http.MethodPost:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case strings.HasPrefix(r.URL.Path, "/runtime/runs/") && r.Method == http.MethodPatch:
			runID := strings.TrimPrefix(r.URL.Path, "/runtime/runs/")
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":         runID,
				"session_id": "sess-1",
				"status":     "completed",
				"phase":      "responded",
				"model":      "gpt-5.4",
				"created_at": ts.Format(time.RFC3339),
			})
		case strings.HasPrefix(r.URL.Path, "/runtime/runs/") && r.Method == http.MethodDelete:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case strings.HasPrefix(r.URL.Path, "/runtime/runs/") && r.Method == http.MethodGet:
			runID := strings.TrimPrefix(r.URL.Path, "/runtime/runs/")
			sessionID := "sess-1"
			if strings.HasPrefix(runID, "run-broadcast-") {
				sessionID = "sess-broadcast"
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":         runID,
				"session_id": sessionID,
				"status":     "completed",
				"phase":      "responded",
				"model":      "gpt-5.4",
				"created_at": ts.Format(time.RFC3339),
			})
		case r.URL.Path == "/runtime/approvals":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{{
					"id":         "approval-1",
					"run_id":     "run-1",
					"session_id": "sess-1",
					"status":     "pending",
					"tool_calls": []map[string]any{{"id": "tool-1", "name": "shell"}},
					"created_at": ts.Format(time.RFC3339),
				}},
				"count": 1,
			})
		case r.URL.Path == "/runtime/approvals/approval-1" && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":         "approval-1",
				"run_id":     "run-1",
				"session_id": "sess-1",
				"status":     "pending",
				"tool_calls": []map[string]any{{"id": "tool-1", "name": "shell"}},
				"created_at": ts.Format(time.RFC3339),
			})
		case r.URL.Path == "/runtime/approvals/approval-1/resolve":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			status := fmt.Sprint(body["status"])
			if status == "<nil>" {
				status = fmt.Sprint(body["Status"])
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"id": "approval-1", "status": status})
		case r.URL.Path == "/runtime/sessions":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"items": []map[string]any{
					{"id": "sess-old", "key": "cli-old", "model": "gpt-5.4", "message_count": 2, "created_at": ts.Add(-50 * 24 * time.Hour).Format(time.RFC3339), "updated_at": ts.Add(-40 * 24 * time.Hour).Format(time.RFC3339)},
					{"id": "sess-1", "key": "cli", "model": "gpt-5.4", "message_count": 2, "created_at": time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339), "updated_at": time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)},
				},
				"count": 2,
			})
		case strings.HasPrefix(r.URL.Path, "/runtime/sessions/sess-1/messages"):
			writeJSONResponse(t, w, http.StatusOK, []map[string]any{{"role": "user", "content": "Hello", "created_at": ts.Format(time.RFC3339)}, {"role": "assistant", "content": "Hi there", "created_at": ts.Format(time.RFC3339)}})
		case strings.HasPrefix(r.URL.Path, "/runtime/sessions/sess-1"):
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"id":         "sess-1",
				"key":        "cli",
				"model":      "gpt-5.4",
				"created_at": ts.Format(time.RFC3339),
				"updated_at": ts.Format(time.RFC3339),
				"messages":   []map[string]any{{"role": "user", "content": "Hello", "created_at": ts.Format(time.RFC3339)}, {"role": "assistant", "content": "Hi there", "created_at": ts.Format(time.RFC3339)}},
			})
		case strings.HasPrefix(r.URL.Path, "/runtime/memory/status"):
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"store_type": "in-memory", "entry_count": 2, "index_ready": true})
		case strings.HasPrefix(r.URL.Path, "/runtime/memory/index"):
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"status": "ok", "indexed": 2})
		case strings.HasPrefix(r.URL.Path, "/runtime/memory/records"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			key := fmt.Sprint(body["key"])
			if strings.TrimSpace(key) == "" {
				key = "mem-auto-1"
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"key": key, "value": fmt.Sprint(body["value"])})
		case strings.HasPrefix(r.URL.Path, "/runtime/memory/"):
			key := strings.TrimPrefix(r.URL.Path, "/runtime/memory/")
			if r.Method == http.MethodDelete {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"deleted": key})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"key": key, "value": "198.51.100.42"})
		case r.URL.Path == "/runtime/memory":
			items := []map[string]any{{"key": "deploy_server", "value": "198.51.100.42"}, {"key": "deploy_note", "value": "remember deploy checklist"}}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
		case r.URL.Path == "/runtime/projects":
			writeJSONResponse(t, w, http.StatusOK, []map[string]any{{"name": "demo", "directory": "/tmp/demo-project", "git_repo": "https://example.com/demo.git"}})
		case strings.HasPrefix(r.URL.Path, "/runtime/projects/demo"):
			if r.Method == http.MethodDelete {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"deleted": "demo"})
				return
			}
			if r.Method == http.MethodPatch {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"old_name": "demo", "name": "demo-renamed"})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"name": "demo", "directory": "/tmp/demo-project", "git_repo": "https://example.com/demo.git"})
		case r.URL.Path == modelsCompletionsPath:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Hello from fake model",
					},
				}},
				"usage": map[string]any{
					"total_tokens": 21,
				},
			})
		case r.URL.Path == modelsBasePath && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"providers": []map[string]any{
					{
						"name":           "default",
						"api":            "openai-completions",
						"base_url":       "https://api.default.example/v1",
						"default_model":  "gpt-5.4",
						"has_key":        true,
						"api_keys_count": 1,
						"timeout":        "30s",
						"header_count":   1,
						"source":         "yaml",
						"mutable":        false,
						"config_scope":   "openai_compat",
						"capability_matrix": map[string]any{
							"provider_name":          "default",
							"provider_api":           "openai-completions",
							"model":                  "gpt-5.4",
							"display_name":           "GPT-5.4",
							"context_window":         200000,
							"max_output_tokens":      8192,
							"supports_system_prompt": true,
							"supports_temperature":   true,
							"supports_max_tokens":    true,
							"supports_tools":         true,
							"supports_tool_replay":   true,
							"supports_vision":        true,
							"supports_reasoning":     true,
							"supports_streaming":     true,
							"supports_json_mode":     true,
							"supports_embeddings":    false,
							"source":                 "operator",
						},
					},
					{
						"name":           "custom-openai",
						"api":            "openai-completions",
						"base_url":       "https://api.example.com/v1",
						"default_model":  "gpt-5.4",
						"has_key":        true,
						"api_keys_count": 1,
						"timeout":        "45s",
						"header_count":   0,
						"source":         "yaml",
						"mutable":        true,
						"config_scope":   "providers",
						"capability_matrix": map[string]any{
							"provider_name":          "custom-openai",
							"provider_api":           "openai-completions",
							"model":                  "gpt-5.4",
							"display_name":           "GPT-5.4",
							"context_window":         200000,
							"max_output_tokens":      8192,
							"supports_system_prompt": true,
							"supports_temperature":   true,
							"supports_max_tokens":    true,
							"supports_tools":         true,
							"supports_tool_replay":   true,
							"supports_vision":        true,
							"supports_reasoning":     true,
							"supports_streaming":     true,
							"supports_json_mode":     true,
							"supports_embeddings":    false,
							"source":                 "operator",
						},
					},
				},
				"count":               2,
				"default_provider":    "custom-openai",
				"agent_default_model": "gpt-5.4",
			})
		case r.URL.Path == modelsRouterPath:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"profiles": []map[string]any{
					{"id": "custom-openai/default", "provider": "custom-openai", "priority": 100, "context_window": 200000, "max_output_tokens": 8192, "enabled": true, "supports": map[string]bool{"tools": true, "vision": true}},
					{"id": "default/default", "provider": "default", "priority": 200, "context_window": 200000, "max_output_tokens": 8192, "enabled": true, "supports": map[string]bool{"tools": true, "vision": true}},
				},
				"count":            2,
				"default_provider": "custom-openai",
			})
		case r.URL.Path == modelsValidatePath:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"valid":   true,
				"message": "provider validated",
				"models":  []map[string]any{{"provider": "custom-openai", "model": "gpt-5.4", "display_name": "GPT-5.4", "context_window": 200000, "max_output": 8192, "capabilities": []string{"tools", "vision"}}},
			})
		case r.URL.Path == modelsTestChatPath:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "reply": "Hello from fake model", "latency_ms": 18, "tokens": 21})
		case r.URL.Path == modelsBasePath && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": fmt.Sprint(body["name"])})
		case r.URL.Path == modelsBasePath+"/custom-openai" && r.Method == http.MethodPut:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "custom-openai"})
		case r.URL.Path == modelsBasePath+"/custom-openai" && r.Method == http.MethodDelete:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "custom-openai"})
		case r.URL.Path == "/operator/agents" && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"name": "ops-bot", "description": "Ops bot", "system_prompt": "You are an ops agent.", "model": "gpt-5.4", "tools": []string{"shell"}, "skills": []string{"summarize"}}}, "count": 1})
		case r.URL.Path == "/operator/agents" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": fmt.Sprint(body["name"])})
		case r.URL.Path == "/operator/agents/ops-bot":
			if r.Method == http.MethodDelete {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "ops-bot"})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"agent": map[string]any{"name": "ops-bot", "description": "Ops bot", "system_prompt": "You are an ops agent.", "model": "gpt-5.4", "tools": []string{"shell"}, "skills": []string{"summarize"}}})
		case r.URL.Path == "/operator/agents/ops-bot/bind":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "agent": "ops-bot", "channel": "slack"})
		case r.URL.Path == "/operator/discovery/peers":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"address": "peer-a:16280", "status": "ok", "version": "test-v1"}}, "count": 1})
		case r.URL.Path == "/operator/pairing":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"channel": "slack", "user_id": "u-1", "status": "pending", "code": "PAIR-123", "created_at": ts.Format(time.RFC3339)}}, "count": 1})
		case r.URL.Path == "/operator/pairing/initiate":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"record": map[string]any{"channel": "slack", "user_id": "u-1", "status": "pending", "code": "PAIR-123", "created_at": ts.Format(time.RFC3339)}})
		case r.URL.Path == "/operator/pairing/verify":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"record": map[string]any{"channel": "slack", "user_id": "u-1", "status": "verified", "code": "PAIR-123", "created_at": ts.Format(time.RFC3339)}})
		case strings.HasPrefix(r.URL.Path, "/operator/pairing/"):
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true})
		case r.URL.Path == "/operator/browser/sessions" && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "browser-1", "capability": "browser", "created_at": ts.Format(time.RFC3339)}}, "count": 1})
		case r.URL.Path == "/operator/browser/sessions" && r.Method == http.MethodPost:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"session_id": "browser-1"})
		case r.URL.Path == "/operator/browser/sessions/browser-1":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "session_id": "browser-1", "capability": "browser"})
		case r.URL.Path == "/operator/browser/sessions/browser-1/tabs":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "tab-1", "title": "Example Domain", "url": "https://example.com"}}, "count": 1})
		case r.URL.Path == "/operator/browser/sessions/browser-1/screenshot":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		case r.URL.Path == "/operator/capabilities":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"manifest": map[string]any{"name": "browser"}, "health": map[string]any{"status": "ready", "message": "ok"}}}})
		case r.URL.Path == "/operator/sandbox/images":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"images": []string{"python:3.12-slim", "ubuntu:24.04"}})
		case r.URL.Path == "/operator/sandbox/status":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"available": true, "allowed_images": []string{"python:3.12-slim"}, "runtime": "docker"})
		case r.URL.Path == "/operator/sandbox/exec":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"stdout": "hello\n", "stderr": "", "exit_code": 0, "timed_out": false, "duration": "5ms"})
		case r.URL.Path == "/operator/webhooks" && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "wh-1", "url": "https://example.com/hook", "events": []string{"run.completed"}, "enabled": true, "created_at": ts.Format(time.RFC3339)}}, "count": 1})
		case r.URL.Path == "/operator/webhooks" && r.Method == http.MethodPost:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"id": "wh-1"})
		case r.URL.Path == "/operator/webhooks/wh-1":
			if r.Method == http.MethodDelete {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "id": "wh-1"})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"webhook": map[string]any{"id": "wh-1", "url": "https://example.com/hook", "events": []string{"run.completed"}, "secret": "secret-token", "enabled": true, "created_at": ts.Format(time.RFC3339)}, "recent_deliveries": []map[string]any{{"id": "deliv-1", "event": "run.completed", "status": 200, "delivered_at": ts.Format(time.RFC3339)}}})
		case r.URL.Path == "/operator/webhooks/wh-1/test":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "status": 200, "id": "wh-1"})
		case r.URL.Path == logsStreamPath:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			event, err := json.Marshal(map[string]any{
				"id":         "evt-stream-1",
				"type":       "run.completed",
				"run_id":     "run-1",
				"session_id": "sess-1",
				"time":       ts.Format(time.RFC3339),
			})
			if err != nil {
				t.Fatalf("marshal stream event: %v", err)
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", event); err != nil {
				t.Fatalf("write stream event: %v", err)
			}
		case r.URL.Path == "/runtime/events":
			if strings.TrimSpace(r.URL.Query().Get("channel")) != "" {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "evt-ch-1", "type": "channel.message.sent", "channel": "slack", "run_id": "run-1", "session_id": "sess-1", "time": ts.Format(time.RFC3339)}}})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "evt-1", "type": "run.completed", "run_id": "run-1", "session_id": "sess-1", "time": ts.Format(time.RFC3339)}}})
		case r.URL.Path == channelsBasePath && r.Method == http.MethodGet:
			enabled := true
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"name": "slack", "config": map[string]any{"type": "slack", "bot_token": "***"}, "enabled": enabled, "source": "yaml"}}, "count": 1})
		case r.URL.Path == channelsHealthPath:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"name": "slack", "state": "connected", "active_runs": 1}, {"name": "discord-alerts", "state": "configured"}}, "count": 2})
		case r.URL.Path == channelsBasePath+"/validate":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"valid": true, "message": "channel is healthy", "status": "connected"})
		case r.URL.Path == channelsBasePath+"/test-message":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "message": "delivered"})
		case r.URL.Path == channelsBasePath && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": fmt.Sprint(body["name"])})
		case strings.HasPrefix(r.URL.Path, channelsBasePath+"/") && r.Method == http.MethodDelete:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": strings.TrimPrefix(r.URL.Path, channelsBasePath+"/")})
		case r.URL.Path == operatorDevicesPairPath:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{
				"device_id":  "linux-test-device",
				"channel":    "desktopd",
				"code":       "PAIR-DEV-123",
				"status":     "pending",
				"created_at": ts.Format(time.RFC3339),
				"expires_at": ts.Add(10 * time.Minute).Format(time.RFC3339),
			})
		case r.URL.Path == hooksBasePath+"/events":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"trigger": "run.completed", "description": "Fires when a run completes successfully and delivery is ready.", "category": "run", "allowed_phases": []string{"post"}, "can_block": false, "supports_async": true}}, "count": 1})
		case r.URL.Path == hooksBasePath && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "hook-complete", "name": "Completion webhook", "enabled": true, "trigger": "run.completed", "kind": "http", "phase": "post", "url": "https://example.com/hook", "timeout": 10, "retry_count": 2, "created_at": ts.Format(time.RFC3339)}}, "count": 1})
		case r.URL.Path == hooksBasePath+"/hook-complete/results":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"hook_id": "hook-complete", "hook_name": "Completion webhook", "hook_kind": "http", "trigger": "run.completed", "phase": "post", "status": "ok", "summary": "Hook delivered", "executed_at": ts.Format(time.RFC3339), "duration": 1000000}}, "count": 1})
		case r.URL.Path == hooksBasePath+"/hook-complete/fire":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"result": map[string]any{"hook_id": "hook-complete", "hook_name": "Completion webhook", "hook_kind": "http", "trigger": "run.completed", "phase": "post", "status": "ok", "summary": "Hook delivered", "executed_at": ts.Format(time.RFC3339)}})
		case r.URL.Path == hooksBasePath+"/hook-complete/replay":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"result": map[string]any{"hook_id": "hook-complete", "hook_name": "Completion webhook", "hook_kind": "http", "trigger": "run.completed", "phase": "post", "status": "ok", "summary": "Replayed payload", "executed_at": ts.Format(time.RFC3339)}})
		case r.URL.Path == automationPath+"/hook/hook-complete":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"item": map[string]any{"id": "hook-complete", "kind": "hook", "name": "Completion webhook", "enabled": true}, "error_signatures": []map[string]any{{"signature": "timeout contacting webhook", "count": 2, "last_occurred_at": ts.Format(time.RFC3339), "last_error": "timeout contacting webhook"}}})
		case r.URL.Path == hooksBasePath+"/hook-complete" && r.Method == http.MethodDelete:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"deleted": "hook-complete"})
		case r.URL.Path == "/operator/discovery/peers/status":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"tailscale": true, "mdns": false, "static_peers": []string{"peer-a:16280"}})
		case r.URL.Path == "/operator/discovery/peers/setup":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "message": "updated"})
		case r.URL.Path == "/operator/automation/items":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "cron-1", "kind": "cron", "name": "Daily report", "enabled": true, "source_kind": "cli", "schedule": "every 1h", "last_run_at": ts.Format(time.RFC3339), "next_run_at": ts.Add(time.Hour).Format(time.RFC3339)}}, "count": 1, "services": map[string]any{"cron": map[string]any{"available": true, "running": true, "count": 1}}})
		case r.URL.Path == "/operator/automation/items/cron/cron-1":
			if r.Method == http.MethodPatch {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"enabled": body["enabled"]})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"item": map[string]any{"id": "cron-1", "kind": "cron", "name": "Daily report", "enabled": true, "schedule": "every 1h"}, "recent_executions": []map[string]any{{"occurred_at": ts.Format(time.RFC3339), "status": "completed", "summary": "Generated report", "verification_status": "passed"}}, "run_path": "/runtime/runs/run-1", "can_replay": true})
		case r.URL.Path == "/operator/automation/templates":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "daily-report-briefing", "kind": "cron", "name": "Daily Report Briefing", "headline": "Daily team summary"}}, "count": 1})
		case r.URL.Path == "/operator/cron/jobs" && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"job": map[string]any{"id": "cron-1", "name": body["name"], "next_run_at": ts.Add(time.Hour).Format(time.RFC3339)}})
		case r.URL.Path == "/operator/cron/jobs/cron-1":
			if r.Method == http.MethodPatch {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"id": "cron-1", "enabled": body["enabled"]})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "id": "cron-1"})
		case r.URL.Path == "/operator/cron/jobs/cron-1/run":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "id": "cron-1"})
		case r.URL.Path == "/operator/skills":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "summarize", "name": "Summarize", "version": "1.0.0", "status": "installed", "trust": "builtin", "install_dir": "/tmp/skills/summarize"}}, "count": 1})
		case strings.HasPrefix(r.URL.Path, "/operator/skills/catalog"):
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"id": "summarize", "name": "Summarize", "version": "1.0.0", "summary": "Summarize long text.", "installed": true}}, "count": 1})
		case r.URL.Path == "/operator/skills/install":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "skill_id": "summarize", "version": "1.0.0", "install_dir": "/tmp/skills/summarize"})
		case r.URL.Path == "/operator/skills/summarize":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"deleted": "summarize"})
		case r.URL.Path == "/runtime/tools":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"name": "shell", "description": "Run a shell command", "side_effect_class": "process", "source": "builtin", "eligible": true}}})
		case r.URL.Path == "/operator/plugins" && r.Method == http.MethodGet:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"items": []map[string]any{{"name": "example-plugin", "version": "0.1.0", "description": "Example plugin", "enabled": true, "component_counts": map[string]any{"tools": 1}}}, "count": 1})
		case r.URL.Path == "/operator/plugins" && r.Method == http.MethodPost:
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "example-plugin"})
		case r.URL.Path == "/operator/plugins/example-plugin":
			if r.Method == http.MethodDelete {
				writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "example-plugin"})
				return
			}
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"plugin": map[string]any{"name": "example-plugin", "version": "0.1.0", "description": "Example plugin", "enabled": true}, "components": []map[string]any{{"kind": "tool", "name": "echo"}}, "component_counts": map[string]any{"tools": 1}})
		case r.URL.Path == "/operator/plugins/example-plugin/enable":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "example-plugin"})
		case r.URL.Path == "/operator/plugins/example-plugin/disable":
			writeJSONResponse(t, w, http.StatusOK, map[string]any{"ok": true, "name": "example-plugin"})
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}

func envURLPlaceholder() string { return `"url": "http://` }

func errorsAs(err error, target any) bool {
	return errors.As(err, target)
}
