package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	replpkg "github.com/fulcrus/hopclaw/internal/cli/repl"
	"github.com/fulcrus/hopclaw/internal/version"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// ---------------------------------------------------------------------------
// doctor command — diagnose common issues
// ---------------------------------------------------------------------------

const (
	doctorConnectTimeout  = 3 * time.Second
	doctorValidateTimeout = 8 * time.Second
	staleLockAge          = 1 * time.Hour
	minFreeDiskBytes      = 100 * 1024 * 1024 // 100 MB
	lockFileExtension     = ".lock"
)

type doctorSection string

const (
	doctorSectionAuth         doctorSection = "auth"
	doctorSectionConfig       doctorSection = "config"
	doctorSectionConnectivity doctorSection = "connectivity"
	doctorSectionSkills       doctorSection = "skills"
	doctorSectionStorage      doctorSection = "storage"
	doctorSectionSecurity     doctorSection = "security"
	doctorSectionPlatform     doctorSection = "platform"
)

type doctorCheckFunc func() checkResult

type doctorSectionSpec struct {
	Use    string
	Short  string
	Checks []doctorCheckFunc
}

var doctorSectionOrder = []doctorSection{
	doctorSectionAuth,
	doctorSectionConfig,
	doctorSectionConnectivity,
	doctorSectionSkills,
	doctorSectionStorage,
	doctorSectionSecurity,
	doctorSectionPlatform,
}

var doctorSectionSpecs = map[doctorSection]doctorSectionSpec{
	doctorSectionAuth: {
		Use:   "auth",
		Short: "Check authentication configuration",
		Checks: []doctorCheckFunc{
			checkAuthConfiguration,
			checkAuthProfile,
			checkAPIKeys,
		},
	},
	doctorSectionConfig: {
		Use:   "config",
		Short: "Check config validity",
		Checks: []doctorCheckFunc{
			checkVersion,
			checkUpdatePolicy,
			checkAvailableUpdate,
			checkConfigFile,
			checkConfigSyntax,
			checkConfigMigration,
			checkModelConfig,
		},
	},
	doctorSectionConnectivity: {
		Use:   "connectivity",
		Short: "Check provider and channel connectivity",
		Checks: []doctorCheckFunc{
			checkGateway,
			checkGatewayHealth,
			checkProviderConnectivity,
			checkChannelHealth,
			checkChannelWebhooks,
		},
	},
	doctorSectionSkills: {
		Use:   "skills",
		Short: "Check skill runtime dependencies",
		Checks: []doctorCheckFunc{
			checkSkillDirectory,
			checkSkillDependencies,
		},
	},
	doctorSectionStorage: {
		Use:   "storage",
		Short: "Check storage health and integrity",
		Checks: []doctorCheckFunc{
			checkStateDir,
			checkRuntimeDatabase,
			checkControlDatabase,
			checkKnowledgeDatabase,
			checkKnowledgeIndexes,
			checkDurableFactsSummary,
			checkAuditDatabase,
			checkSessionLocks,
			checkStateIntegrity,
			checkDiskSpace,
		},
	},
	doctorSectionSecurity: {
		Use:   "security",
		Short: "Check secret exposure and file safety",
		Checks: []doctorCheckFunc{
			checkSecuritySecretInventory,
			checkSecrets,
			checkConfigPermissions,
		},
	},
	doctorSectionPlatform: {
		Use:   "platform",
		Short: "Check platform and dependency compatibility",
		Checks: []doctorCheckFunc{
			checkPlatformRuntime,
			checkDaemon,
			checkSandboxImage,
			checkPlatformNotes,
		},
	},
}

// deprecatedConfigKeys lists old config keys that have been superseded.
var deprecatedConfigKeys = []string{
	"server.auth_token",
}

// secretPrefixes lists token prefixes that should not appear as plaintext
// values in config files.
var secretPrefixes = []string{
	"sk-",
	"xoxb-",
	"xapp-",
	"sk-ant-",
	"ghp_",
	"gho_",
}

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and fix common issues",
		Long:  "Run a series of checks to diagnose configuration, connectivity, and service issues.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDoctorSections(cmd, doctorSectionOrder)
		},
	}
	cmd.PersistentFlags().Bool("fix", false, "attempt auto-remediation for fixable issues")
	for _, section := range doctorSectionOrder {
		spec := doctorSectionSpecs[section]
		section := section
		cmd.AddCommand(&cobra.Command{
			Use:   spec.Use,
			Short: spec.Short,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runDoctorSections(cmd, []doctorSection{section})
			},
		})
	}
	return cmd
}

type checkResult struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Status   string `json:"status"` // "ok", "warn", "fail"
	Detail   string `json:"detail,omitempty"`
	Fix      string `json:"fix,omitempty"`
}

func runDoctorSections(cmd *cobra.Command, sections []doctorSection) error {
	autoFix, _ := cmd.Flags().GetBool("fix")

	checks := collectDoctorChecksForSections(sections)

	if autoFix {
		checks = applyFixes(checks)
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(checks)
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "HopClaw Doctor (%s on %s/%s)\n\n", version.Version, runtime.GOOS, runtime.GOARCH)
	printDoctorReport(w, checks)
	return nil
}

func collectDoctorChecks() []checkResult {
	return collectDoctorChecksForSections(doctorSectionOrder)
}

func collectDoctorChecksForSections(sections []doctorSection) []checkResult {
	requested := make([]doctorSection, 0, len(sections))
	for _, section := range doctorSectionOrder {
		if len(sections) > 0 && !slices.Contains(sections, section) {
			continue
		}
		requested = append(requested, section)
	}
	checks := make([]checkResult, 0, len(requested)*3)
	for _, section := range requested {
		spec, ok := doctorSectionSpecs[section]
		if !ok {
			continue
		}
		for _, fn := range spec.Checks {
			checks = append(checks, fn())
		}
	}
	return checks
}

func doctorChecksSnapshot() []replpkg.DoctorCheck {
	raw := collectDoctorChecks()
	out := make([]replpkg.DoctorCheck, 0, len(raw))
	for _, item := range raw {
		out = append(out, replpkg.DoctorCheck{
			Category: item.Category,
			Name:     item.Name,
			Status:   item.Status,
			Detail:   item.Detail,
			Fix:      item.Fix,
		})
	}
	return out
}

func printDoctorReport(w io.Writer, checks []checkResult) {
	useColor := doctorUseColor(w)
	for _, c := range checks {
		icon := colorizeDoctor(iconForStatus(c.Status), c.Status, useColor)
		fmt.Fprintf(w, "  %s %-16s", icon, c.Name)
		if detail := strings.TrimSpace(c.Detail); detail != "" {
			fmt.Fprintf(w, " %s", detail)
		}
		fmt.Fprintln(w)
	}

	passed, failed, warned := summarizeDoctorChecks(checks)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %d passed · %d failed · %d warning\n", passed, failed, warned)

	fixes := doctorFixes(checks)
	if len(fixes) == 0 {
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintln(w)
	for idx, fix := range fixes {
		prefix := "  Fix:"
		if idx > 0 {
			prefix = "      "
		}
		fmt.Fprintf(w, "%s %s\n", prefix, fix)
	}
	fmt.Fprintln(w)
}

func iconForStatus(status string) string {
	switch status {
	case "ok":
		return "✓"
	case "warn":
		return "⚠"
	case "fail":
		return "✗"
	default:
		return "?"
	}
}

func summarizeDoctorChecks(checks []checkResult) (passed, failed, warned int) {
	for _, check := range checks {
		switch check.Status {
		case "ok":
			passed++
		case "fail":
			failed++
		case "warn":
			warned++
		}
	}
	return passed, failed, warned
}

func doctorFixes(checks []checkResult) []string {
	seen := make(map[string]struct{}, len(checks))
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		fix := strings.TrimSpace(check.Fix)
		if fix == "" || check.Status == "ok" {
			continue
		}
		if _, ok := seen[fix]; ok {
			continue
		}
		seen[fix] = struct{}{}
		out = append(out, fix)
	}
	return out
}

func doctorUseColor(w io.Writer) bool {
	if flagJSON {
		return false
	}
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

func colorizeDoctor(text, status string, enabled bool) string {
	if !enabled {
		return text
	}
	code := ""
	switch status {
	case "ok":
		code = "32"
	case "warn":
		code = "33"
	case "fail":
		code = "31"
	default:
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func defaultDoctorString(items ...string) string {
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return "release feed"
}
