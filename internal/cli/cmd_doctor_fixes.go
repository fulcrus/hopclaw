package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/daemon"
)

func applyFixes(checks []checkResult) []checkResult {
	for i, c := range checks {
		if c.Status == "ok" || c.Fix == "" {
			continue
		}

		switch c.Fix {
		case "auto:mkdir_state_dir":
			if err := daemon.EnsureStateDir(); err == nil {
				checks[i].Status = "ok"
				checks[i].Detail = fmt.Sprintf("created %s", daemon.StateDir())
				checks[i].Fix = "fixed: created state directory"
			} else {
				checks[i].Fix = fmt.Sprintf("fix failed: %v", err)
			}

		case "auto:clear_stale_locks":
			cleared, err := clearStaleLocks()
			if err == nil {
				checks[i].Status = "ok"
				checks[i].Detail = fmt.Sprintf("cleared %d stale lock file(s)", cleared)
				checks[i].Fix = "fixed: stale locks removed"
			} else {
				checks[i].Fix = fmt.Sprintf("fix failed: %v", err)
			}
		}
	}

	return checks
}

func clearStaleLocks() (int, error) {
	dir := daemon.StateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	cleared := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), lockFileExtension) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > staleLockAge {
			if err := os.Remove(filepath.Join(dir, e.Name())); err == nil {
				cleared++
			}
		}
	}

	return cleared, nil
}
