package gateway

import (
	"context"
	"fmt"
	"strings"

	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/config"
	authzrbac "github.com/fulcrus/hopclaw/contrib/authz-rbac"
)

type gatewayAuthSetup struct {
	chain             *AuthChain
	authSessionStore  AuthSessionStore
	authSessionConfig AuthSessionConfig
	oauth2Provider    *OAuth2Provider
	authzDecider      authz.AuthorizationDecider
	err               error
}

func buildGatewayAuth(cfg Config) gatewayAuthSetup {
	authzDecider, authzErr := buildAuthorizationDecider(cfg)
	setup := gatewayAuthSetup{
		chain:             NewAuthChain(),
		authSessionConfig: defaultAuthSessionConfig(authSessionConfig(cfg.AuthConfig.Session)),
		authzDecider:      authzDecider,
		err:               authzErr,
	}

	var providers []AuthProvider

	if token := strings.TrimSpace(cfg.AuthToken); token != "" {
		providers = append(providers, NewBearerTokenProvider(token))
	}
	if token := strings.TrimSpace(cfg.AuthConfig.BearerToken); token != "" {
		providers = append(providers, NewBearerTokenProvider(token))
	}

	if cfg.AuthConfig.JWT != nil {
		jwtCfg := JWTConfig{
			Secret:    cfg.AuthConfig.JWT.Secret,
			PublicKey: cfg.AuthConfig.JWT.PublicKey,
			Issuer:    cfg.AuthConfig.JWT.Issuer,
			Audience:  cfg.AuthConfig.JWT.Audience,
			Algorithm: cfg.AuthConfig.JWT.Algorithm,
			ClockSkew: cfg.AuthConfig.JWT.ClockSkew,
		}
		provider, err := NewJWTProvider(jwtCfg)
		if err != nil {
			setup.err = fmt.Errorf("jwt auth init: %w", err)
			return setup
		}
		providers = append(providers, provider)
	}

	if len(cfg.AuthConfig.APIKeys) > 0 {
		entries := make([]APIKeyEntry, 0, len(cfg.AuthConfig.APIKeys))
		for _, entry := range cfg.AuthConfig.APIKeys {
			if strings.TrimSpace(entry.Key) == "" {
				continue
			}
			entries = append(entries, APIKeyEntry{
				Key:     entry.Key,
				Name:    entry.Name,
				Scopes:  entry.Scopes,
				Enabled: entry.Enabled,
			})
		}
		if len(entries) > 0 {
			providers = append(providers, NewAPIKeyProvider(entries))
		}
	}

	if cfg.AuthConfig.OAuth2 != nil || cfg.AuthConfig.Session != nil {
		setup.authSessionStore = NewMemoryAuthSessionStore(setup.authSessionConfig)
		providers = append(providers, NewAuthSessionProvider(setup.authSessionStore, setup.authSessionConfig))
	}

	if cfg.AuthConfig.OAuth2 != nil {
		oauthCfg := OAuth2Config{
			Issuer:       cfg.AuthConfig.OAuth2.Issuer,
			ClientID:     cfg.AuthConfig.OAuth2.ClientID,
			ClientSecret: cfg.AuthConfig.OAuth2.ClientSecret,
			RedirectURI:  cfg.AuthConfig.OAuth2.RedirectURI,
			Scopes:       cfg.AuthConfig.OAuth2.Scopes,
			DiscoveryURL: cfg.AuthConfig.OAuth2.DiscoveryURL,
		}
		provider, err := NewOAuth2Provider(oauthCfg, setup.authSessionStore, setup.authSessionConfig)
		if err != nil {
			setup.err = fmt.Errorf("oauth2 auth init: %w", err)
			return setup
		}
		setup.oauth2Provider = provider
	}

	setup.chain = NewAuthChain(providers...)
	return setup
}

func buildAuthorizationDecider(cfg Config) (authz.AuthorizationDecider, error) {
	if cfg.AuthorizationDecider != nil {
		return cfg.AuthorizationDecider, nil
	}
	switch selectedAuthZMode(cfg.AuthZConfig) {
	case "open":
		return authz.OpenDecider{}, nil
	case "rbac":
		if !authRBACConfigured(cfg.AuthConfig.RBAC) {
			return nil, fmt.Errorf("authz mode rbac requires auth.rbac configuration")
		}
		return authzrbac.NewFromConfig(cfg.AuthConfig.RBAC), nil
	case "webhook":
		decider, ok, err := buildConfiguredExternalDecider(cfg)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("authz mode webhook requires authz.webhook configuration")
		}
		return decider, nil
	}

	if decider, ok, err := buildConfiguredExternalDecider(cfg); ok || err != nil {
		return decider, err
	}
	if authRBACConfigured(cfg.AuthConfig.RBAC) {
		return authzrbac.NewFromConfig(cfg.AuthConfig.RBAC), nil
	}
	return authz.OpenDecider{}, nil
}

func selectedAuthZMode(cfg config.AuthZConfig) string {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode != "" {
		return mode
	}
	if strings.TrimSpace(cfg.Webhook.URL) != "" {
		return "webhook"
	}
	return ""
}

func buildConfiguredExternalDecider(cfg Config) (authz.AuthorizationDecider, bool, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.AuthZConfig.Mode))
	if mode == "" && strings.TrimSpace(cfg.AuthZConfig.Webhook.URL) != "" {
		mode = "webhook"
	}
	if mode != "webhook" {
		return nil, false, nil
	}
	delegate, err := authz.NewWebhookDecider(authz.WebhookConfig{
		URL:     cfg.AuthZConfig.Webhook.URL,
		Timeout: cfg.AuthZConfig.Webhook.Timeout,
		Headers: cfg.AuthZConfig.Webhook.Headers,
		Secret:  cfg.AuthZConfig.Webhook.Secret,
	})
	if err != nil {
		return nil, true, fmt.Errorf("authz webhook init: %w", err)
	}
	return authz.ExternalDecider{
		Name:     "WebhookDecider",
		Delegate: delegate,
		Fallback: buildAuthZFallbackDecider(cfg.AuthZConfig, cfg.AuthConfig.RBAC),
	}, true, nil
}

func buildAuthZFallbackDecider(cfg config.AuthZConfig, rbac config.AuthRBACConfig) authz.AuthorizationDecider {
	switch strings.ToLower(strings.TrimSpace(cfg.Fallback)) {
	case "open":
		return authz.OpenDecider{}
	case "rbac":
		if authRBACConfigured(rbac) {
			return authzrbac.NewFromConfig(rbac)
		}
	case "deny":
		return authz.DecisionFunc(func(_ context.Context, _ authz.AuthorizationRequest) (authz.AuthorizationDecision, error) {
			return authz.AuthorizationDecision{
				Allowed: false,
				Reason:  "external authorization backend unavailable",
				Source:  "authz-fallback:deny",
			}, nil
		})
	}
	return nil
}

func authRBACConfigured(cfg config.AuthRBACConfig) bool {
	return strings.TrimSpace(cfg.Mode) != "" ||
		strings.TrimSpace(cfg.DefaultRole) != "" ||
		len(cfg.RoleMetadataKeys) > 0 ||
		len(cfg.GroupMetadataKeys) > 0 ||
		len(cfg.ScopePrefixes) > 0 ||
		len(cfg.GroupRoles) > 0 ||
		len(cfg.Roles) > 0
}

func authSessionConfig(cfg *config.AuthSessionConfig) AuthSessionConfig {
	if cfg == nil {
		return AuthSessionConfig{}
	}
	return AuthSessionConfig{
		CookieName:   cfg.CookieName,
		CookieDomain: cfg.CookieDomain,
		MaxAge:       cfg.MaxAge,
		Secure:       cfg.Secure,
	}
}
