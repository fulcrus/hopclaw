package cli

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/keychain"
)

type targetAuthUpdate struct {
	Token    string
	TokenEnv string
}

func resolveSavedTargetAuthToken(profile savedTargetProfile) (string, error) {
	switch strings.TrimSpace(profile.AuthType) {
	case "", targetAuthTypeNone:
		return "", nil
	case targetAuthTypeBearer:
		if strings.TrimSpace(profile.AuthRef) == "" {
			return "", missingTargetCredentialsError(profile.Name)
		}
		resolved, err := keychain.ResolveSecret(profile.AuthRef)
		if err != nil {
			return "", fmt.Errorf("resolve connection auth for %q: %w", profile.Name, err)
		}
		return strings.TrimSpace(resolved), nil
	default:
		return "", fmt.Errorf("unsupported connection auth type %q", profile.AuthType)
	}
}

func updateSavedTargetCredentials(name string, update targetAuthUpdate) (savedTargetProfile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return savedTargetProfile{}, fmt.Errorf("remote name is required")
	}
	if strings.TrimSpace(update.Token) != "" && strings.TrimSpace(update.TokenEnv) != "" {
		return savedTargetProfile{}, fmt.Errorf("--token and --token-env cannot be used together")
	}

	authRef := ""
	switch {
	case strings.TrimSpace(update.Token) != "":
		ref, err := saveManagedTargetSecret(name, strings.TrimSpace(update.Token))
		if err != nil {
			return savedTargetProfile{}, fmt.Errorf("save remote token: %w", err)
		}
		authRef = ref
	case strings.TrimSpace(update.TokenEnv) != "":
		authRef = "env:" + strings.TrimSpace(update.TokenEnv)
	default:
		return savedTargetProfile{}, fmt.Errorf("login requires --token or --token-env")
	}

	existing, found, err := getSavedTargetProfile(name)
	if err != nil {
		if strings.HasPrefix(authRef, "keychain:") {
			_ = deleteManagedTargetSecret(authRef)
		}
		return savedTargetProfile{}, err
	}
	if !found {
		if strings.HasPrefix(authRef, "keychain:") {
			_ = deleteManagedTargetSecret(authRef)
		}
		return savedTargetProfile{}, fmt.Errorf("remote %q not found", name)
	}

	updated, err := updateSavedTargetProfile(name, func(profile savedTargetProfile) (savedTargetProfile, error) {
		profile.AuthType = targetAuthTypeBearer
		profile.AuthRef = authRef
		return profile, nil
	})
	if err != nil {
		if strings.HasPrefix(authRef, "keychain:") {
			_ = deleteManagedTargetSecret(authRef)
		}
		return savedTargetProfile{}, err
	}
	if existing.AuthRef != authRef {
		_ = deleteManagedTargetSecret(existing.AuthRef)
	}
	return updated, nil
}

func clearSavedTargetCredentials(name string) (savedTargetProfile, error) {
	existing, found, err := getSavedTargetProfile(name)
	if err != nil {
		return savedTargetProfile{}, err
	}
	if !found {
		return savedTargetProfile{}, fmt.Errorf("remote %q not found", strings.TrimSpace(name))
	}
	updated, err := updateSavedTargetProfile(name, func(profile savedTargetProfile) (savedTargetProfile, error) {
		profile.AuthType = targetAuthTypeNone
		profile.AuthRef = ""
		return profile, nil
	})
	if err != nil {
		return savedTargetProfile{}, err
	}
	if err := deleteManagedTargetSecret(existing.AuthRef); err != nil {
		return savedTargetProfile{}, fmt.Errorf("delete remote secret: %w", err)
	}
	return updated, nil
}

func missingTargetCredentialsError(name string) error {
	name = strings.TrimSpace(name)
	return fmt.Errorf("connection %q requires bearer credentials; run `hopclaw remote login %s`", name, name)
}
