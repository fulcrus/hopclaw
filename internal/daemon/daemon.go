// Package daemon provides cross-platform system service management for HopClaw.
// It supports macOS launchd, Linux systemd, and Windows schtasks.
package daemon

// ---------------------------------------------------------------------------
// ServiceManager interface
// ---------------------------------------------------------------------------

// ServiceConfig holds the parameters needed to install the HopClaw service.
type ServiceConfig struct {
	// BinaryPath is the absolute path to the hopclaw binary.
	BinaryPath string

	// ConfigPath is the absolute path to the config file to use.
	ConfigPath string

	// LogPath is the directory for service stdout/stderr logs.
	LogPath string
}

// ServiceStatus describes the current state of the installed service.
type ServiceStatus struct {
	Installed bool
	Running   bool
	PID       int
	Label     string // platform-specific identifier (plist label, unit name, task name)
}

// ServiceManager abstracts platform-specific service lifecycle operations.
type ServiceManager interface {
	Install(cfg ServiceConfig) error
	Uninstall() error
	Start() error
	Stop() error
	Restart() error
	Status() (*ServiceStatus, error)
}

// NewServiceManager returns the ServiceManager for the current OS.
// Each platform provides its own newPlatformManager implementation via
// build-tagged files.
func NewServiceManager() (ServiceManager, error) {
	return newPlatformManager()
}
