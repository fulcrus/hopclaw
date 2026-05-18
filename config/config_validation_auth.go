package config

import (
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/authz"
)

func validateAuthConfig(c Config) error {
	if err := validateAuthZConfig(c.AuthZ, c.Auth.RBAC); err != nil {
		return err
	}
	if c.Auth.JWT != nil {
		alg := strings.ToUpper(strings.TrimSpace(c.Auth.JWT.Algorithm))
		if alg == "" {
			if strings.TrimSpace(c.Auth.JWT.PublicKey) != "" {
				alg = "RS256"
			} else {
				alg = "HS256"
			}
		}
		switch alg {
		case "HS256":
			if strings.TrimSpace(c.Auth.JWT.Secret) == "" {
				return fmt.Errorf("auth.jwt.secret is required for HS256")
			}
		case "RS256":
			if strings.TrimSpace(c.Auth.JWT.PublicKey) == "" {
				return fmt.Errorf("auth.jwt.public_key is required for RS256")
			}
		default:
			return fmt.Errorf("unsupported auth.jwt.algorithm %q", c.Auth.JWT.Algorithm)
		}
	}

	for i, entry := range c.Auth.APIKeys {
		if strings.TrimSpace(entry.Key) == "" {
			return fmt.Errorf("auth.api_keys[%d].key is required", i)
		}
		if strings.TrimSpace(entry.Name) == "" {
			return fmt.Errorf("auth.api_keys[%d].name is required", i)
		}
	}

	if c.Auth.OAuth2 != nil {
		if strings.TrimSpace(c.Auth.OAuth2.ClientID) == "" {
			return fmt.Errorf("auth.oauth2.client_id is required")
		}
		if strings.TrimSpace(c.Auth.OAuth2.RedirectURI) == "" {
			return fmt.Errorf("auth.oauth2.redirect_uri is required")
		}
		if strings.TrimSpace(c.Auth.OAuth2.Issuer) == "" && strings.TrimSpace(c.Auth.OAuth2.DiscoveryURL) == "" {
			return fmt.Errorf("auth.oauth2.issuer or auth.oauth2.discovery_url is required")
		}
	}

	if c.Auth.Session != nil && c.Auth.OAuth2 == nil &&
		strings.TrimSpace(c.Server.AuthToken) == "" &&
		strings.TrimSpace(c.Auth.BearerToken) == "" &&
		c.Auth.JWT == nil &&
		len(c.Auth.APIKeys) == 0 {
		return fmt.Errorf("auth.session requires auth.oauth2 or another primary auth provider")
	}
	if err := validateAuthRBACConfig(c.Auth.RBAC); err != nil {
		return err
	}

	return nil
}

func validateAuthZConfig(cfg AuthZConfig, rbac AuthRBACConfig) error {
	if err := validateNonNegativeDuration("authz.webhook.timeout", cfg.Webhook.Timeout); err != nil {
		return err
	}
	mode := normalizeAuthZMode(cfg.Mode)
	if mode == "" {
		mode = inferredAuthZMode(cfg)
	}
	switch mode {
	case "", "open", "rbac", "webhook":
	default:
		return fmt.Errorf("authz.mode must be one of open, rbac, webhook")
	}
	if authZWebhookConfigured(cfg.Webhook) && normalizeAuthZMode(cfg.Mode) != "" && normalizeAuthZMode(cfg.Mode) != "webhook" {
		return fmt.Errorf("authz.webhook requires authz.mode=webhook when authz.mode is set explicitly")
	}
	if mode == "webhook" {
		if strings.TrimSpace(cfg.Webhook.URL) == "" {
			return fmt.Errorf("authz.webhook.url is required when authz.mode=webhook")
		}
		if err := validateWebhookURL("authz.webhook.url", cfg.Webhook.URL); err != nil {
			return err
		}
	}
	if mode == "rbac" && !authRBACConfigured(rbac) {
		return fmt.Errorf("authz.mode=rbac requires auth.rbac configuration")
	}
	switch normalizeAuthZFallback(cfg.Fallback) {
	case "", "deny", "open":
	case "rbac":
		if !authRBACConfigured(rbac) {
			return fmt.Errorf("authz.fallback=rbac requires auth.rbac configuration")
		}
	default:
		return fmt.Errorf("authz.fallback must be one of open, rbac, deny")
	}
	return nil
}

func validateAuthRBACConfig(cfg AuthRBACConfig) error {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "" && mode != "overlay" && mode != "replace" {
		return fmt.Errorf("auth.rbac.mode must be one of overlay, replace")
	}
	if strings.TrimSpace(cfg.DefaultRole) == "" && cfg.DefaultRole != "" {
		return fmt.Errorf("auth.rbac.default_role must not be blank")
	}
	for group, role := range cfg.GroupRoles {
		if strings.TrimSpace(group) == "" {
			return fmt.Errorf("auth.rbac.group_roles contains an empty group name")
		}
		if strings.TrimSpace(role) == "" {
			return fmt.Errorf("auth.rbac.group_roles[%q] role is required", group)
		}
	}

	roleIndex := make(map[string]AuthRBACRoleConfig, len(cfg.Roles))
	for i, role := range cfg.Roles {
		name := strings.ToLower(strings.TrimSpace(role.Name))
		if name == "" {
			return fmt.Errorf("auth.rbac.roles[%d].name is required", i)
		}
		if _, exists := roleIndex[name]; exists {
			return fmt.Errorf("duplicate auth.rbac.roles name %q", name)
		}
		for j, grant := range role.Grants {
			resourceValue := strings.TrimSpace(grant.Resource)
			if resourceValue == "" {
				return fmt.Errorf("auth.rbac.roles[%d].grants[%d].resource is required", i, j)
			}
			if _, ok := authz.ParseResource(resourceValue); !ok {
				return fmt.Errorf(
					"auth.rbac.roles[%d].grants[%d].resource must be one of %s",
					i,
					j,
					strings.Join(authz.ResourceNames(authz.AllResources()), ", "),
				)
			}
			if len(grant.Permissions) == 0 {
				return fmt.Errorf("auth.rbac.roles[%d].grants[%d].permissions is required", i, j)
			}
			for k, permission := range grant.Permissions {
				permissionValue := strings.TrimSpace(permission)
				if permissionValue == "" {
					return fmt.Errorf("auth.rbac.roles[%d].grants[%d].permissions[%d] is required", i, j, k)
				}
				if _, ok := authz.ParseAction(permissionValue); !ok {
					return fmt.Errorf(
						"auth.rbac.roles[%d].grants[%d].permissions[%d] must be one of %s",
						i,
						j,
						k,
						strings.Join(authz.ActionNames(authz.AllActions()), ", "),
					)
				}
			}
		}
		roleIndex[name] = role
	}

	visitState := make(map[string]int, len(roleIndex))
	var visit func(name string) error
	visit = func(name string) error {
		switch visitState[name] {
		case 1:
			return fmt.Errorf("auth.rbac.roles inheritance cycle detected at %q", name)
		case 2:
			return nil
		}
		role, ok := roleIndex[name]
		if !ok {
			return nil
		}
		visitState[name] = 1
		for _, parent := range role.Extends {
			parentName := strings.ToLower(strings.TrimSpace(parent))
			if parentName == "" {
				return fmt.Errorf("auth.rbac.roles[%q].extends contains an empty role name", name)
			}
			if err := visit(parentName); err != nil {
				return err
			}
		}
		visitState[name] = 2
		return nil
	}
	for name := range roleIndex {
		if err := visit(name); err != nil {
			return err
		}
	}

	return nil
}

func authRBACConfigured(cfg AuthRBACConfig) bool {
	return strings.TrimSpace(cfg.Mode) != "" ||
		strings.TrimSpace(cfg.DefaultRole) != "" ||
		len(cfg.RoleMetadataKeys) > 0 ||
		len(cfg.GroupMetadataKeys) > 0 ||
		len(cfg.ScopePrefixes) > 0 ||
		len(cfg.GroupRoles) > 0 ||
		len(cfg.Roles) > 0
}

func authZWebhookConfigured(cfg AuthZWebhookConfig) bool {
	return strings.TrimSpace(cfg.URL) != ""
}

func inferredAuthZMode(cfg AuthZConfig) string {
	if authZWebhookConfigured(cfg.Webhook) {
		return "webhook"
	}
	return ""
}

func normalizeAuthZMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "open", "rbac", "webhook":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeAuthZFallback(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "deny", "open", "rbac":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
