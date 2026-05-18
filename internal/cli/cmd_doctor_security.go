package cli

import (
	"fmt"
	"os"

	"github.com/fulcrus/hopclaw/controlplane"
)

func checkSecuritySecretInventory() checkResult {
	cfg, loaded, err := loadDoctorConfig()
	if !loaded {
		return checkResult{
			Category: "security",
			Name:     "Secret exposure",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}
	if err != nil {
		return checkResult{
			Category: "security",
			Name:     "Secret exposure",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot load config: %v", err),
		}
	}
	return runDoctorProbeByID(doctorProbeSecretInventory, controlplane.StorageSummary{}, cfg.SecretInventory())
}

func checkConfigPermissions() checkResult {
	p := resolveConfigPath()
	if p == "" {
		return checkResult{
			Category: "security",
			Name:     "Config permissions",
			Status:   "ok",
			Detail:   "no config file; skipped",
		}
	}

	info, err := os.Stat(p)
	if err != nil {
		return checkResult{
			Category: "security",
			Name:     "Config permissions",
			Status:   "warn",
			Detail:   fmt.Sprintf("cannot stat config: %v", err),
		}
	}

	perms := info.Mode().Perm()
	if perms&0o077 != 0 {
		return checkResult{
			Category: "security",
			Name:     "Config permissions",
			Status:   "warn",
			Detail:   fmt.Sprintf("%s is too open (%#o)", p, perms),
			Fix:      fmt.Sprintf("run 'chmod 600 %s' to restrict config access", p),
		}
	}

	return checkResult{
		Category: "security",
		Name:     "Config permissions",
		Status:   "ok",
		Detail:   fmt.Sprintf("%s (%#o)", p, perms),
	}
}
