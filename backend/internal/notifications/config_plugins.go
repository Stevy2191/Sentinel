package notifications

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// This file builds notification plugins from a persisted models.NotificationConfig
// (database-backed configuration) rather than from environment variables. The
// env-based constructors (NewEmailPlugin, ...) remain the startup/fallback path;
// these *FromConfig constructors power the admin-configured channels and the
// per-channel connection test.

// NewEmailPluginFromConfig builds an EmailPlugin from explicit SMTP settings.
func NewEmailPluginFromConfig(host string, port int, user, password, from string, tlsEnabled bool) *EmailPlugin {
	if port <= 0 {
		port = defaultSMTPPort
	}
	if from == "" {
		from = user
	}
	// With no explicit recipient list the config carries, send to the from/user
	// address (matches the env plugin's self-send fallback).
	to := []string{from}
	return &EmailPlugin{
		host:       host,
		port:       port,
		user:       user,
		password:   password,
		from:       from,
		to:         to,
		tlsEnabled: tlsEnabled,
		logger:     log.Default(),
	}
}

// NewSlackPluginFromConfig builds a SlackPlugin from an explicit webhook URL.
func NewSlackPluginFromConfig(webhookURL string) (*SlackPlugin, error) {
	u, err := url.ParseRequestURI(webhookURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("slack webhook URL is not a valid https URL: %q", webhookURL)
	}
	return &SlackPlugin{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: defaultSlackTimeout},
		logger:     log.Default(),
	}, nil
}

// NewDiscordPluginFromConfig builds a DiscordPlugin from an explicit webhook URL.
func NewDiscordPluginFromConfig(webhookURL string) (*DiscordPlugin, error) {
	u, err := url.ParseRequestURI(webhookURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("discord webhook URL is not a valid https URL: %q", webhookURL)
	}
	return &DiscordPlugin{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: defaultDiscordTimeout},
		logger:     log.Default(),
	}, nil
}

// NewTelegramPluginFromConfig builds a TelegramPlugin from explicit credentials.
func NewTelegramPluginFromConfig(botToken, chatID string) *TelegramPlugin {
	return &TelegramPlugin{
		botToken:   botToken,
		chatID:     chatID,
		apiBase:    defaultTelegramAPIBase,
		httpClient: &http.Client{Timeout: defaultTelegramTimeout},
		logger:     log.Default(),
	}
}

// NewNtfyPluginFromConfig builds an NtfyPlugin from an explicit server + topic.
func NewNtfyPluginFromConfig(serverURL, topic string) *NtfyPlugin {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		serverURL = defaultNtfyURL
	}
	return &NtfyPlugin{
		url:        serverURL,
		topic:      topic,
		httpClient: &http.Client{Timeout: defaultNtfyTimeout},
		logger:     log.Default(),
	}
}

// NewWebhookPluginFromConfig builds a WebhookPlugin from an explicit URL and
// optional custom headers.
func NewWebhookPluginFromConfig(webhookURL string, headers map[string]string) (*WebhookPlugin, error) {
	u, err := url.ParseRequestURI(webhookURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("webhook URL is not a valid http(s) URL: %q", webhookURL)
	}
	return &WebhookPlugin{
		webhookURL: webhookURL,
		headers:    headers,
		httpClient: &http.Client{Timeout: defaultWebhookTimeout},
		logger:     log.Default(),
	}, nil
}

// deref returns the pointed-to string, or "" if the pointer is nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// BuildPluginFromConfig constructs the appropriate NotificationPlugin for a
// stored config. The config should already have passed Validate(); this still
// guards against malformed URLs. It does not consult the Enabled flag — callers
// decide whether to register or merely test the resulting plugin.
func BuildPluginFromConfig(cfg models.NotificationConfig) (NotificationPlugin, error) {
	switch cfg.Channel {
	case "email":
		port := 0
		if cfg.SMTPPort != nil {
			port = *cfg.SMTPPort
		}
		return NewEmailPluginFromConfig(
			deref(cfg.SMTPHost), port, deref(cfg.SMTPUser),
			deref(cfg.SMTPPassword), deref(cfg.SMTPFrom), true,
		), nil
	case "slack":
		return NewSlackPluginFromConfig(deref(cfg.WebhookURL))
	case "discord":
		return NewDiscordPluginFromConfig(deref(cfg.WebhookURL))
	case "telegram":
		return NewTelegramPluginFromConfig(deref(cfg.TelegramBotToken), deref(cfg.TelegramChatID)), nil
	case "ntfy":
		return NewNtfyPluginFromConfig(deref(cfg.NtfyURL), deref(cfg.NtfyTopic)), nil
	case "webhook":
		return NewWebhookPluginFromConfig(deref(cfg.WebhookURL), cfg.CustomHeaders)
	default:
		return nil, errors.New("unknown notification channel: " + cfg.Channel)
	}
}

// testMessage is the synthetic payload used by connection tests.
func testMessage() *NotificationMessage {
	return &NotificationMessage{
		MonitorName:    "Sentinel Test",
		MonitorURL:     "https://example.com",
		Status:         "recovered",
		PreviousStatus: "down",
		Message:        "This is a test notification from Sentinel.",
		Timestamp:      time.Now(),
		ResponseTimeMs: 42,
	}
}
