package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fulcrus/hopclaw/skill"
)

func (a *App) SearchSkillCatalog(ctx context.Context, query string) ([]skill.RegistrySkill, error) {
	if a == nil || a.skillHub == nil {
		return nil, nil
	}
	return a.skillHub.Search(ctx, strings.TrimSpace(query))
}

func (a *App) InstalledSkillLocks() ([]skill.InstalledSkillLock, error) {
	if a == nil || a.skillHub == nil {
		return nil, nil
	}
	return a.skillHub.Installed()
}

func (a *App) InstallSkill(ctx context.Context, rawSource, name, version string) (*skill.InstallResult, error) {
	if a == nil || a.skillHub == nil {
		return nil, nil
	}
	skillID, source, localSource := resolveSkillInstallTarget(rawSource, name)
	if skillID == "" && !localSource {
		return nil, fmt.Errorf("skill id is required")
	}

	var (
		result *skill.InstallResult
		err    error
	)
	if localSource {
		installer, ok := a.skillHub.(skill.LocalSourceInstaller)
		if !ok {
			return nil, fmt.Errorf("local skill source install is not supported")
		}
		result, err = installer.InstallFromSource(ctx, skill.InstallRequest{
			SkillID: skillID,
			Version: strings.TrimSpace(version),
		}, source)
	} else {
		result, err = a.skillHub.Install(ctx, skill.InstallRequest{
			SkillID: skillID,
			Version: strings.TrimSpace(version),
		})
	}
	if err != nil {
		return nil, err
	}
	if a.SkillService != nil {
		if _, refreshErr := a.SkillService.Refresh(ctx); refreshErr != nil {
			return nil, refreshErr
		}
	}
	return result, nil
}

func (a *App) RemoveInstalledSkill(ctx context.Context, name string) error {
	if a == nil || a.skillHub == nil {
		return nil
	}
	if err := a.skillHub.Remove(strings.TrimSpace(name)); err != nil {
		return err
	}
	if a.SkillService != nil {
		if _, err := a.SkillService.Refresh(ctx); err != nil {
			return err
		}
	}
	return nil
}

func resolveSkillInstallTarget(rawSource, rawName string) (skillID string, source string, localSource bool) {
	skillID = strings.TrimSpace(rawName)
	source = strings.TrimSpace(rawSource)
	if source == "" {
		return skillID, source, false
	}
	if _, err := os.Stat(source); err == nil {
		return skillID, source, true
	}
	if skillID == "" {
		skillID = source
	}
	return skillID, source, false
}
