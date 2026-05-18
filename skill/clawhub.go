package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

type ClawHubClient interface {
	Search(ctx context.Context, query string) ([]RegistrySkill, error)
	Install(ctx context.Context, req InstallRequest) (*InstallResult, error)
	Update(ctx context.Context, skillID string) (*InstallResult, error)
	Pin(ctx context.Context, skillID, version string) error
	Sync(ctx context.Context) error
	Remove(skillID string) error
	Installed() ([]InstalledSkillLock, error)
	Publish(ctx context.Context, req PublishRequest) (*PublishResult, error)
}

type LocalSourceInstaller interface {
	InstallFromSource(ctx context.Context, req InstallRequest, source string) (*InstallResult, error)
}

type ClawHubLayout struct {
	Root string
}

func DefaultClawHubRoot(home string) string {
	return filepath.Join(home, ".hopclaw", "clawhub")
}

func (l ClawHubLayout) IndexDir() string {
	return filepath.Join(l.Root, "index")
}

func (l ClawHubLayout) CacheDir() string {
	return filepath.Join(l.Root, "cache")
}

func (l ClawHubLayout) BundleDir(skillID, version string) string {
	return filepath.Join(l.CacheDir(), "bundles", skillID, version)
}

func (l ClawHubLayout) BlobDir() string {
	return filepath.Join(l.CacheDir(), "blobs")
}

func (l ClawHubLayout) InstallsDir() string {
	return filepath.Join(l.Root, "installs")
}

func (l ClawHubLayout) InstallDir(skillID, version string) string {
	return filepath.Join(l.InstallsDir(), skillID, version)
}

func (l ClawHubLayout) LocksDir() string {
	return filepath.Join(l.Root, "locks")
}

func (l ClawHubLayout) SkillsLockPath() string {
	return filepath.Join(l.LocksDir(), "skills.lock.json")
}

type LocalInstaller struct {
	Layout ClawHubLayout
}

func (i LocalInstaller) InstallFromBundle(ctx context.Context, req InstallRequest, bundleDir string) (*InstallResult, error) {
	if req.Root != "" {
		i.Layout.Root = req.Root
	}
	if i.Layout.Root == "" {
		return nil, fmt.Errorf("installer root is required")
	}
	if req.SkillID == "" || req.Version == "" {
		return nil, fmt.Errorf("skill id and version are required")
	}

	dest := i.Layout.InstallDir(req.SkillID, req.Version)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, err
	}
	if err := copyDir(ctx, bundleDir, dest); err != nil {
		return nil, err
	}

	var installSteps []InstallStepResult
	spec, parseErr := ParseDir(dest)
	switch {
	case parseErr == nil:
		if len(spec.OpenClaw.Install) > 0 {
			executor := DefaultInstallExecutor()
			steps, execErr := executor.Execute(ctx, dest, spec.OpenClaw.Install)
			if execErr != nil {
				logging.DebugIfErr(os.RemoveAll(dest), "remove clawhub dest dir failed")
				return nil, fmt.Errorf("execute skill installers: %w", execErr)
			}
			installSteps = steps
		}
	}

	lock, err := i.LoadLock()
	if err != nil {
		logging.DebugIfErr(os.RemoveAll(dest), "remove clawhub dest dir failed")
		return nil, err
	}
	lock = upsertLock(lock, InstalledSkillLock{
		SkillID:     req.SkillID,
		Version:     req.Version,
		InstallDir:  dest,
		BundleDir:   bundleDir,
		Pinned:      false,
		InstalledAt: time.Now().UTC(),
	})
	lock.GeneratedAt = time.Now().UTC()
	if err := i.SaveLock(lock); err != nil {
		logging.DebugIfErr(os.RemoveAll(dest), "remove clawhub dest dir failed")
		return nil, err
	}

	return &InstallResult{
		SkillID:        req.SkillID,
		Version:        req.Version,
		InstallDir:     dest,
		LockFilePath:   i.Layout.SkillsLockPath(),
		InstallerSteps: installSteps,
	}, nil
}

func (i LocalInstaller) PinVersion(skillID, version string) error {
	lock, err := i.LoadLock()
	if err != nil {
		return err
	}
	for idx := range lock.Skills {
		if lock.Skills[idx].SkillID == skillID && lock.Skills[idx].Version == version {
			lock.Skills[idx].Pinned = true
		}
	}
	lock.GeneratedAt = time.Now().UTC()
	return i.SaveLock(lock)
}

func (i LocalInstaller) LoadLock() (*SkillsLockFile, error) {
	path := i.Layout.SkillsLockPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SkillsLockFile{}, nil
		}
		return nil, err
	}
	var lock SkillsLockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("decode lock file: %w", err)
	}
	return &lock, nil
}

func (i LocalInstaller) SaveLock(lock *SkillsLockFile) error {
	if err := os.MkdirAll(i.Layout.LocksDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(i.Layout.SkillsLockPath(), data, 0o644)
}

func upsertLock(lock *SkillsLockFile, entry InstalledSkillLock) *SkillsLockFile {
	if lock == nil {
		lock = &SkillsLockFile{}
	}
	replaced := false
	for idx := range lock.Skills {
		if lock.Skills[idx].SkillID == entry.SkillID {
			lock.Skills[idx] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		lock.Skills = append(lock.Skills, entry)
	}
	sort.Slice(lock.Skills, func(i, j int) bool {
		if lock.Skills[i].SkillID != lock.Skills[j].SkillID {
			return lock.Skills[i].SkillID < lock.Skills[j].SkillID
		}
		return compareSemver(parseSemver(lock.Skills[i].Version), parseSemver(lock.Skills[j].Version)) < 0
	})
	return lock
}

func copyDir(ctx context.Context, src, dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
