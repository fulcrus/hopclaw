package config

import "time"

type ChannelsConfig struct {
	Feishu        FeishuChannelConfig        `yaml:"feishu"`
	Slack         SlackChannelConfig         `yaml:"slack"`
	Discord       DiscordChannelConfig       `yaml:"discord"`
	Telegram      TelegramChannelConfig      `yaml:"telegram"`
	Webhook       WebhookChannelConfig       `yaml:"webhook"`
	WhatsApp      WhatsAppChannelConfig      `yaml:"whatsapp"`
	Signal        SignalChannelConfig        `yaml:"signal"`
	IMessage      IMessageChannelConfig      `yaml:"imessage"`
	LINE          LINEChannelConfig          `yaml:"line"`
	MSTeams       MSTeamsChannelConfig       `yaml:"msteams"`
	GoogleChat    GoogleChatChannelConfig    `yaml:"googlechat"`
	IRC           IRCChannelConfig           `yaml:"irc"`
	Matrix        MatrixChannelConfig        `yaml:"matrix"`
	Mattermost    MattermostChannelConfig    `yaml:"mattermost"`
	NextcloudTalk NextcloudTalkChannelConfig `yaml:"nextcloud_talk"`
	Nostr         NostrChannelConfig         `yaml:"nostr"`
	BlueBubbles   BlueBubblesChannelConfig   `yaml:"bluebubbles"`
	SynologyChat  SynologyChatChannelConfig  `yaml:"synology_chat"`
	Tlon          TlonChannelConfig          `yaml:"tlon"`
	Twitch        TwitchChannelConfig        `yaml:"twitch"`
	Zalo          ZaloChannelConfig          `yaml:"zalo"`
	ZaloUser      ZaloUserChannelConfig      `yaml:"zalouser"`
}

type CommonChannelConfig struct {
	DMPolicy          string        `yaml:"dm_policy"`
	AllowFrom         []string      `yaml:"allow_from"`
	GroupPolicy       string        `yaml:"group_policy"`
	GroupAllowFrom    []string      `yaml:"group_allow_from"`
	RequireMention    *bool         `yaml:"require_mention"`
	GroupSessionScope string        `yaml:"group_session_scope"`
	ReplyInThread     string        `yaml:"reply_in_thread"`
	DedupeTTL         time.Duration `yaml:"dedupe_ttl"`
	DedupeDir         string        `yaml:"dedupe_dir"`
}

type SlackChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	BotToken            string `yaml:"bot_token"`
	AppToken            string `yaml:"app_token"`
}

type DiscordChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	BotToken            string `yaml:"bot_token"`
}

type TelegramChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	BotToken            string `yaml:"bot_token"`
}

type FeishuChannelConfig struct {
	Enabled           *bool                          `yaml:"enabled"`
	DefaultAccount    string                         `yaml:"default_account"`
	AppID             string                         `yaml:"app_id"`
	AppSecret         string                         `yaml:"app_secret"`
	EncryptKey        string                         `yaml:"encrypt_key"`
	VerificationToken string                         `yaml:"verification_token"`
	Domain            string                         `yaml:"domain"`
	ConnectionMode    string                         `yaml:"connection_mode"`
	DMPolicy          string                         `yaml:"dm_policy"`
	AllowFrom         []string                       `yaml:"allow_from"`
	GroupPolicy       string                         `yaml:"group_policy"`
	GroupAllowFrom    []string                       `yaml:"group_allow_from"`
	RequireMention    *bool                          `yaml:"require_mention"`
	GroupSessionScope string                         `yaml:"group_session_scope"`
	ReplyInThread     string                         `yaml:"reply_in_thread"`
	DedupeTTL         time.Duration                  `yaml:"dedupe_ttl"`
	DedupeDir         string                         `yaml:"dedupe_dir"`
	Accounts          map[string]FeishuAccountConfig `yaml:"accounts"`
}

type FeishuAccountConfig struct {
	Enabled           *bool    `yaml:"enabled"`
	Name              string   `yaml:"name"`
	AppID             string   `yaml:"app_id"`
	AppSecret         string   `yaml:"app_secret"`
	EncryptKey        string   `yaml:"encrypt_key"`
	VerificationToken string   `yaml:"verification_token"`
	Domain            string   `yaml:"domain"`
	ConnectionMode    string   `yaml:"connection_mode"`
	DMPolicy          string   `yaml:"dm_policy"`
	AllowFrom         []string `yaml:"allow_from"`
	GroupPolicy       string   `yaml:"group_policy"`
	GroupAllowFrom    []string `yaml:"group_allow_from"`
	RequireMention    *bool    `yaml:"require_mention"`
	GroupSessionScope string   `yaml:"group_session_scope"`
	ReplyInThread     string   `yaml:"reply_in_thread"`
}

type WhatsAppChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	PhoneID             string `yaml:"phone_id"`
	APIToken            string `yaml:"api_token"`
	BaseURL             string `yaml:"base_url"`
}

type SignalChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	BaseURL             string `yaml:"base_url"`
	Number              string `yaml:"number"`
	AuthToken           string `yaml:"auth_token"`
}

type IMessageChannelConfig struct {
	Enabled *bool  `yaml:"enabled"`
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
}

type LINEChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	ChannelSecret       string `yaml:"channel_secret"`
	ChannelToken        string `yaml:"channel_token"`
}

type MSTeamsChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	AppID               string `yaml:"app_id"`
	Password            string `yaml:"password"`
}

type GoogleChatChannelConfig struct {
	Enabled         *bool  `yaml:"enabled"`
	ServiceAccount  string `yaml:"service_account"`
	WebhookURL      string `yaml:"webhook_url"`
	VerificationKey string `yaml:"verification_key"`
}

type IRCChannelConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	Server   string `yaml:"server"`
	Nick     string `yaml:"nick"`
	Password string `yaml:"password"`
	UseTLS   *bool  `yaml:"use_tls"`
	Channels string `yaml:"channels"`
}

type MatrixChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	HomeServer          string `yaml:"homeserver"`
	UserID              string `yaml:"user_id"`
	AccessToken         string `yaml:"access_token"`
}

type MattermostChannelConfig struct {
	CommonChannelConfig `yaml:",inline"`
	Enabled             *bool  `yaml:"enabled"`
	BaseURL             string `yaml:"base_url"`
	BotToken            string `yaml:"bot_token"`
	WebSocketURL        string `yaml:"websocket_url"`
}

type NextcloudTalkChannelConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	BaseURL  string `yaml:"base_url"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type NostrChannelConfig struct {
	Enabled    *bool    `yaml:"enabled"`
	PrivateKey string   `yaml:"private_key"`
	Relays     []string `yaml:"relays"`
}

type BlueBubblesChannelConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	BaseURL  string `yaml:"base_url"`
	Password string `yaml:"password"`
}

type SynologyChatChannelConfig struct {
	Enabled    *bool  `yaml:"enabled"`
	BaseURL    string `yaml:"base_url"`
	WebhookURL string `yaml:"webhook_url"`
	BotToken   string `yaml:"bot_token"`
}

type TlonChannelConfig struct {
	Enabled  *bool  `yaml:"enabled"`
	ShipURL  string `yaml:"ship_url"`
	ShipCode string `yaml:"ship_code"`
}

type TwitchChannelConfig struct {
	Enabled    *bool  `yaml:"enabled"`
	OAuthToken string `yaml:"oauth_token"`
	Nick       string `yaml:"nick"`
	Channels   string `yaml:"channels"`
}

type ZaloChannelConfig struct {
	Enabled      *bool  `yaml:"enabled"`
	AppID        string `yaml:"app_id"`
	SecretKey    string `yaml:"secret_key"`
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token"`
}

type ZaloUserChannelConfig struct {
	Enabled *bool  `yaml:"enabled"`
	Cookie  string `yaml:"cookie"`
	IMEI    string `yaml:"imei"`
	BaseURL string `yaml:"base_url"`
}
