package cli

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type gatewayAccess struct {
	Address   string
	BaseURL   string
	AuthToken string
}

func resolveGatewayAccess() (gatewayAccess, error) {
	client, err := NewGatewayClient()
	if err != nil {
		return gatewayAccess{}, err
	}
	return gatewayAccessFromClient(client), nil
}

func resolveGatewayAccessForTarget(ctx context.Context, name string) (gatewayAccess, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return resolveGatewayAccess()
	}
	if isBuiltinLocalTargetName(name) {
		return gatewayAccess{}, fmt.Errorf("dashboard is not available for local")
	}
	if profile, found, err := getSavedTargetProfile(name); err != nil {
		return gatewayAccess{}, err
	} else if found {
		token, err := resolveSavedTargetAuthToken(profile)
		if err != nil {
			return gatewayAccess{}, err
		}
		return gatewayAccess{
			Address:   strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(profile.BaseURL, "http://"), "https://")),
			BaseURL:   strings.TrimSpace(profile.BaseURL),
			AuthToken: token,
		}, nil
	}
	if strings.Contains(name, "://") {
		baseURL, err := normalizeManagedTargetURL(name)
		if err != nil {
			return gatewayAccess{}, err
		}
		return gatewayAccess{
			Address: strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://")),
			BaseURL: baseURL,
		}, nil
	}
	targets, err := selectableLocalTargets(ctx)
	if err != nil {
		return gatewayAccess{}, err
	}
	for _, item := range targets {
		if strings.EqualFold(item.Name, name) {
			token, err := resolveInteractiveTargetAuthToken(item)
			if err != nil {
				return gatewayAccess{}, err
			}
			return gatewayAccess{
				Address:   strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(item.BaseURL, "http://"), "https://")),
				BaseURL:   strings.TrimSpace(item.BaseURL),
				AuthToken: token,
			}, nil
		}
	}
	return gatewayAccess{}, fmt.Errorf("connection %q not found", name)
}

func gatewayAccessFromClient(client *GatewayClient) gatewayAccess {
	if client == nil {
		return gatewayAccess{}
	}

	baseURL := strings.TrimRight(strings.TrimSpace(client.BaseURL), "/")
	address := ""
	if parsed, err := url.Parse(baseURL); err == nil {
		address = strings.TrimSpace(parsed.Host)
	}
	if address == "" {
		address = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(baseURL, "http://"), "https://"))
	}

	return gatewayAccess{
		Address:   address,
		BaseURL:   baseURL,
		AuthToken: strings.TrimSpace(client.AuthToken),
	}
}

func buildGatewayURL(baseURL, path string, query url.Values) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	path = "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	if baseURL == "" {
		baseURL = "http://" + defaultGatewayAddr
	}

	fullURL := baseURL + path
	if len(query) == 0 {
		return fullURL
	}
	return fullURL + "?" + query.Encode()
}

func dashboardURLs(access gatewayAccess) (displayURL, openURL string) {
	displayURL = buildGatewayURL(access.BaseURL, dashboardPath, nil)
	openURL = displayURL
	if token := strings.TrimSpace(access.AuthToken); token != "" {
		query := url.Values{}
		query.Set("token", token)
		openURL = buildGatewayURL(access.BaseURL, dashboardPath, query)
	}
	return displayURL, openURL
}

func buildQRQuery(session, channel string) url.Values {
	query := url.Values{}
	if value := strings.TrimSpace(session); value != "" {
		query.Set("session", value)
	}
	if value := strings.TrimSpace(channel); value != "" {
		query.Set("channel", value)
	}
	return query
}

func maskDisplayToken(token string) string {
	const minVisibleLen = 12
	if len(token) < minVisibleLen {
		return strings.Repeat("*", len(token))
	}
	const visibleChars = 4
	return token[:visibleChars] + strings.Repeat("*", len(token)-visibleChars*2) + token[len(token)-visibleChars:]
}
