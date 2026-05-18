package plugin

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateManifest performs lightweight public-contract validation that plugin
// authors can run before packaging a manifest.
func ValidateManifest(m Manifest) []error {
	var errs []error

	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, fmt.Errorf("manifest name is required"))
	}
	if version := strings.TrimSpace(m.Version); version != "" {
		if err := validateLooseVersion(version); err != nil {
			errs = append(errs, fmt.Errorf("invalid version %q: %w", m.Version, err))
		}
	}

	for name, provider := range m.Providers {
		if strings.TrimSpace(provider.API) == "" {
			errs = append(errs, fmt.Errorf("provider %q: api is required", name))
		}
	}
	for _, command := range m.Commands {
		if strings.TrimSpace(command.Name) == "" {
			errs = append(errs, fmt.Errorf("command name is required"))
		}
		if strings.TrimSpace(command.Exec) == "" {
			label := strings.TrimSpace(command.Name)
			if label == "" {
				label = "<unnamed>"
			}
			errs = append(errs, fmt.Errorf("command %q: exec is required", label))
		}
	}

	return errs
}

func validateLooseVersion(version string) error {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return fmt.Errorf("must not be empty")
	}

	core := version
	if idx := strings.IndexAny(core, "-+"); idx >= 0 {
		core = core[:idx]
	}

	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return fmt.Errorf("must use 1 to 3 numeric segments")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("contains an empty numeric segment")
		}
		if _, err := strconv.Atoi(part); err != nil {
			return fmt.Errorf("segment %q is not numeric", part)
		}
	}
	return nil
}
