package daemon

import (
	"os"
	"path/filepath"
	"runtime"
)

// ---------------------------------------------------------------------------
// Standard directory paths for HopClaw user-level data
// ---------------------------------------------------------------------------

const appDirName = ".hopclaw"

// StateDir returns the root state directory (~/.hopclaw/).
func StateDir() string {
	return filepath.Join(homeDir(), appDirName)
}

// LogDir returns the log directory (~/.hopclaw/logs/).
func LogDir() string {
	return filepath.Join(StateDir(), "logs")
}

// ConfigDir returns the config directory (~/.hopclaw/).
func ConfigDir() string {
	return StateDir()
}

// DataDir returns the data directory (~/.hopclaw/data/).
func DataDir() string {
	return filepath.Join(StateDir(), "data")
}

// ConfigFilePath returns the default config file path (~/.hopclaw/config.yaml).
func ConfigFilePath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

// PIDFilePath returns the PID file path (~/.hopclaw/hopclaw.pid).
func PIDFilePath() string {
	return filepath.Join(StateDir(), "hopclaw.pid")
}

// EnsureStateDir creates the state directory and subdirectories if they do
// not exist.
func EnsureStateDir() error {
	dirs := []string{StateDir(), LogDir(), DataDir()}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	if runtime.GOOS == "windows" {
		return os.Getenv("USERPROFILE")
	}
	return "."
}
