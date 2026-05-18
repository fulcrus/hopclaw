package config

func channelTextField(configKey, label string, required bool, placeholder string) SetupChannelField {
	return SetupChannelField{
		ID:          configKey,
		ConfigKey:   configKey,
		Label:       label,
		Required:    required,
		Placeholder: placeholder,
		Type:        SetupChannelFieldString,
	}
}

func channelSecretField(configKey, label string, required bool, placeholder string) SetupChannelField {
	return SetupChannelField{
		ID:          configKey,
		ConfigKey:   configKey,
		Label:       label,
		Required:    required,
		Secret:      true,
		Placeholder: placeholder,
		Type:        SetupChannelFieldString,
	}
}

func channelBoolField(configKey, label string) SetupChannelField {
	return SetupChannelField{
		ID:        configKey,
		ConfigKey: configKey,
		Label:     label,
		Type:      SetupChannelFieldBool,
	}
}

func channelStringListField(configKey, label string, required bool, placeholder string) SetupChannelField {
	return SetupChannelField{
		ID:          configKey,
		ConfigKey:   configKey,
		Label:       label,
		Required:    required,
		Placeholder: placeholder,
		Type:        SetupChannelFieldStringList,
	}
}

func appendChannelFieldGroups(groups ...[]SetupChannelField) []SetupChannelField {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total == 0 {
		return nil
	}
	out := make([]SetupChannelField, 0, total)
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

func operatorPolicyFields(includeDM, includeGroup, includeRequireMention bool) []SetupChannelField {
	fields := make([]SetupChannelField, 0, 5)
	if includeDM {
		fields = append(fields, channelTextField("dm_policy", "DM Policy", false, "open | allowlist | pairing"))
	}
	if includeGroup {
		fields = append(fields, channelTextField("group_policy", "Group Policy", false, "open | allowlist | disabled"))
		if includeRequireMention {
			fields = append(fields, channelBoolField("require_mention", "Require Mention"))
		}
		fields = append(fields,
			channelTextField("group_session_scope", "Session Scope", false, "group | group_sender | group_thread | group_thread_sender"),
			channelTextField("reply_in_thread", "Reply In Thread", false, "enabled | disabled"),
		)
	}
	return fields
}

func cloneSetupChannelFields(fields []SetupChannelField) []SetupChannelField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]SetupChannelField, len(fields))
	copy(out, fields)
	return out
}

func EffectiveOperatorChannelFields(profile ChannelProfile) []SetupChannelField {
	if len(profile.OperatorFields) > 0 {
		return cloneSetupChannelFields(profile.OperatorFields)
	}
	return cloneSetupChannelFields(profile.Fields)
}

func channelOperatorFields(channelID string, setupFields []SetupChannelField) []SetupChannelField {
	switch normalizeChannelCatalogID(channelID) {
	case "slack":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelSecretField("bot_token", "Bot Token", true, "xoxb-..."),
				channelSecretField("app_token", "App Token", false, "xapp-... (Socket Mode)"),
			},
			operatorPolicyFields(true, true, false),
		)
	case "discord":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelSecretField("bot_token", "Bot Token", true, ""),
			},
			operatorPolicyFields(true, true, false),
		)
	case "telegram":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelSecretField("bot_token", "Bot Token", true, ""),
			},
			operatorPolicyFields(true, true, false),
		)
	case "feishu":
		return []SetupChannelField{
			channelTextField("app_id", "App ID", true, ""),
			channelSecretField("app_secret", "App Secret", true, ""),
			channelSecretField("encrypt_key", "Encrypt Key", false, ""),
			channelSecretField("verification_token", "Verification Token", false, ""),
			channelTextField("domain", "Domain", false, "feishu or lark"),
		}
	case "webhook":
		return []SetupChannelField{
			channelTextField("callback_url", "Callback URL", true, ""),
			channelSecretField("secret", "Secret", false, ""),
		}
	case "whatsapp":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelTextField("phone_id", "Phone Number ID", true, ""),
				channelSecretField("api_token", "API Token", true, ""),
				channelTextField("base_url", "Base URL", false, "graph.facebook.com"),
			},
			operatorPolicyFields(true, false, false),
		)
	case "signal":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelTextField("base_url", "API Base URL", true, "http://127.0.0.1:8080"),
				channelTextField("number", "Phone Number", true, "+1234567890"),
				channelSecretField("auth_token", "Auth Token", false, ""),
			},
			operatorPolicyFields(true, true, false),
		)
	case "imessage":
		return []SetupChannelField{
			channelTextField("base_url", "BlueBubbles URL", true, ""),
			channelSecretField("api_key", "API Key", true, ""),
		}
	case "line":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelSecretField("channel_secret", "Channel Secret", true, ""),
				channelSecretField("channel_token", "Channel Token", true, ""),
			},
			operatorPolicyFields(true, true, false),
		)
	case "msteams":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelTextField("app_id", "App ID", true, ""),
				channelSecretField("password", "App Password", true, ""),
			},
			operatorPolicyFields(true, true, false),
		)
	case "googlechat":
		return []SetupChannelField{
			channelTextField("service_account", "Service Account JSON Path", false, ""),
			channelTextField("webhook_url", "Webhook URL", false, ""),
			channelSecretField("verification_key", "Verification Key", false, ""),
		}
	case "irc":
		return []SetupChannelField{
			channelTextField("server", "Server", true, "irc.libera.chat:6697"),
			channelTextField("nick", "Nickname", true, ""),
			channelSecretField("password", "Password", false, ""),
			channelTextField("channels", "Channels", false, "#channel1,#channel2"),
			channelBoolField("use_tls", "Use TLS"),
		}
	case "matrix":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelTextField("homeserver", "Homeserver URL", true, "https://matrix.org"),
				channelTextField("user_id", "User ID", true, "@bot:matrix.org"),
				channelSecretField("access_token", "Access Token", true, ""),
			},
			operatorPolicyFields(false, true, true),
		)
	case "mattermost":
		return appendChannelFieldGroups(
			[]SetupChannelField{
				channelTextField("base_url", "Base URL", true, ""),
				channelSecretField("bot_token", "Bot Token", true, ""),
				channelTextField("websocket_url", "WebSocket URL", false, ""),
			},
			operatorPolicyFields(true, true, true),
		)
	case "nextcloud_talk":
		return []SetupChannelField{
			channelTextField("base_url", "Base URL", true, ""),
			channelTextField("username", "Username", true, ""),
			channelSecretField("password", "App Password", true, ""),
		}
	case "nostr":
		return []SetupChannelField{
			channelSecretField("private_key", "Private Key", true, ""),
			channelStringListField("relays", "Relay URLs", true, "wss://relay1.example\nwss://relay2.example"),
		}
	case "bluebubbles":
		return []SetupChannelField{
			channelTextField("base_url", "Base URL", true, ""),
			channelSecretField("password", "Password", true, ""),
		}
	case "synology_chat":
		return []SetupChannelField{
			channelTextField("base_url", "NAS URL", true, ""),
			channelTextField("webhook_url", "Webhook URL", false, ""),
			channelSecretField("bot_token", "Bot Token", true, ""),
		}
	case "tlon":
		return []SetupChannelField{
			channelTextField("ship_url", "Ship URL", true, ""),
			channelSecretField("ship_code", "Ship Code", true, ""),
		}
	case "twitch":
		return []SetupChannelField{
			channelSecretField("oauth_token", "OAuth Token", true, "oauth:..."),
			channelTextField("nick", "Bot Username", true, ""),
			channelTextField("channels", "Channels", false, "#channel1,#channel2"),
		}
	case "zalo":
		return []SetupChannelField{
			channelTextField("app_id", "App ID", true, ""),
			channelSecretField("secret_key", "Secret Key", true, ""),
			channelSecretField("access_token", "Access Token", true, ""),
			channelSecretField("refresh_token", "Refresh Token", false, ""),
		}
	case "zalouser":
		return []SetupChannelField{
			channelSecretField("cookie", "Zalo Session Cookie", true, ""),
			channelSecretField("imei", "Device IMEI", false, ""),
			channelTextField("base_url", "Base URL", false, ""),
		}
	default:
		return cloneSetupChannelFields(setupFields)
	}
}
