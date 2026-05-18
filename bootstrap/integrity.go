package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (a *App) ValidateIntegrity() error {
	if a == nil {
		return errors.New("bootstrap integrity check failed: app is nil")
	}
	issues := make([]string, 0, 12)
	if a.Runtime == nil {
		issues = append(issues, "runtime service missing")
	}
	if a.Sessions == nil {
		issues = append(issues, "session store missing")
	}
	if a.Runs == nil {
		issues = append(issues, "run store missing")
	}
	if a.Bus == nil {
		issues = append(issues, "event bus missing")
	}
	if a.Capabilities == nil {
		issues = append(issues, "capability registry missing")
	}
	if a.ExtensionRegistry == nil {
		issues = append(issues, "extension registry missing")
	}
	if a.Channels == nil {
		issues = append(issues, "channel manager missing")
	}
	if a.Gateway == nil {
		issues = append(issues, "gateway missing")
	}
	if a.Handler == nil {
		issues = append(issues, "http handler missing")
	}
	if a.GrantStore == nil {
		issues = append(issues, "grant store missing")
	}
	if a.NodeRegistry == nil {
		issues = append(issues, "node registry missing")
	}
	if a.EffectiveConfigResolver() == nil {
		issues = append(issues, "effective config resolver missing")
	}
	if a.Runtime != nil && a.Runtime.EffectiveConfigSnapshot() == nil {
		issues = append(issues, "runtime effective config snapshot missing")
	}
	if a.Gateway != nil {
		if err := a.Gateway.ValidateIntegrity(); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if len(issues) == 0 {
		return nil
	}
	return fmt.Errorf("bootstrap integrity check failed: %s", strings.Join(issues, ", "))
}

func validateAppIntegrity(ctx context.Context, app *App) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if app == nil {
		return errors.New("bootstrap integrity check failed: app is nil")
	}
	return app.ValidateIntegrity()
}
