package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/config"
)

// Reloader handles hot-reloading of application components when config changes.
type Reloader struct {
	app *App
}

// NewReloader creates a reload orchestrator for the given application.
func NewReloader(app *App) *Reloader {
	return &Reloader{app: app}
}

// HandleReload applies config changes through the effective-config path.
func (r *Reloader) HandleReload(old, new config.Config, changes config.ChangeSet) error {
	if changes.Fatal {
		return fmt.Errorf("config contains non-reloadable changes (%s), restart required",
			strings.Join(changes.Sections(), ", "))
	}
	if r == nil || r.app == nil {
		return nil
	}
	if err := r.app.ApplyBaseConfig(context.Background(), new); err != nil {
		return fmt.Errorf("apply effective config: %w", err)
	}
	log.Info("config reload complete", "sections", changes.Sections())
	return nil
}
