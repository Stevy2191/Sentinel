package models

import (
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
)

// ValidNotificationChannels lists the channel names a config row may use.
var ValidNotificationChannels = map[string]bool{
	"email":    true,
	"slack":    true,
	"discord":  true,
	"telegram": true,
	"ntfy":     true,
	"webhook":  true,
}

// NotificationConfig is the persisted, per-channel configuration for a
// notification delivery channel. Secret fields (SMTP password, Telegram token,
// webhook URLs) are stored here but stripped from list responses via HideSecrets.
type NotificationConfig struct {
	ID      uuid.UUID `json:"id" gorm:"column:id;type:uuid;default:gen_random_uuid();primaryKey"`
	Channel string    `json:"channel" gorm:"column:channel;not null"` // email, slack, discord, telegram, ntfy, webhook
	Enabled bool      `json:"enabled" gorm:"column:enabled"`

	// Email/SMTP
	SMTPHost     *string `json:"smtp_host" gorm:"column:smtp_host"`
	SMTPPort     *int    `json:"smtp_port" gorm:"column:smtp_port"`
	SMTPUser     *string `json:"smtp_user" gorm:"column:smtp_user"`
	SMTPPassword *string `json:"smtp_password,omitempty" gorm:"column:smtp_password"` // never returned in list responses
	SMTPFrom     *string `json:"smtp_from" gorm:"column:smtp_from"`

	// Slack/Discord/Webhook/Ntfy (generic URL)
	WebhookURL *string `json:"webhook_url,omitempty" gorm:"column:webhook_url"` // hidden in list, returned on single GET

	// Telegram
	TelegramBotToken *string `json:"telegram_bot_token,omitempty" gorm:"column:telegram_bot_token"` // never returned in list responses
	TelegramChatID   *string `json:"telegram_chat_id" gorm:"column:telegram_chat_id"`

	// Ntfy
	NtfyURL   *string `json:"ntfy_url" gorm:"column:ntfy_url"`
	NtfyTopic *string `json:"ntfy_topic" gorm:"column:ntfy_topic"`

	// Custom headers applied to outgoing webhook requests.
	CustomHeaders StringMap `json:"custom_headers,omitempty" gorm:"column:custom_headers;type:jsonb"`

	// Connection test status
	LastTestAt      *time.Time `json:"last_test_at" gorm:"column:last_test_at"`
	LastTestSuccess *bool      `json:"last_test_success" gorm:"column:last_test_success"`
	LastTestError   *string    `json:"last_test_error" gorm:"column:last_test_error"`

	CreatedAt time.Time `json:"created_at" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
}

// TableName tells GORM which table backs the NotificationConfig model.
func (NotificationConfig) TableName() string {
	return "notification_configs"
}

// Validate checks that the config has the fields its channel requires.
func (nc *NotificationConfig) Validate() error {
	if nc.Channel == "" {
		return errors.New("channel is required")
	}
	if !ValidNotificationChannels[nc.Channel] {
		return errors.New("unknown channel: " + nc.Channel)
	}

	switch nc.Channel {
	case "email":
		if nc.SMTPHost == nil || *nc.SMTPHost == "" {
			return errors.New("SMTP host is required for email")
		}
		if nc.SMTPPort == nil || *nc.SMTPPort < 1 || *nc.SMTPPort > 65535 {
			return errors.New("SMTP port must be 1-65535")
		}
		if nc.SMTPUser == nil || *nc.SMTPUser == "" {
			return errors.New("SMTP user is required")
		}
		if nc.SMTPFrom == nil || *nc.SMTPFrom == "" {
			return errors.New("SMTP from address is required")
		}

	case "slack", "discord", "webhook":
		if nc.WebhookURL == nil || *nc.WebhookURL == "" {
			return errors.New(nc.Channel + " webhook URL is required")
		}
		if u, err := url.ParseRequestURI(*nc.WebhookURL); err != nil || u.Host == "" {
			return errors.New("invalid webhook URL format")
		}

	case "telegram":
		if nc.TelegramBotToken == nil || *nc.TelegramBotToken == "" {
			return errors.New("Telegram bot token is required")
		}
		if nc.TelegramChatID == nil || *nc.TelegramChatID == "" {
			return errors.New("Telegram chat ID is required")
		}

	case "ntfy":
		if nc.NtfyTopic == nil || *nc.NtfyTopic == "" {
			return errors.New("Ntfy topic is required")
		}
	}

	return nil
}

// HideSecrets removes sensitive fields so a config is safe to include in list
// responses. Call this before returning configs to the frontend in bulk.
func (nc *NotificationConfig) HideSecrets() {
	nc.SMTPPassword = nil
	nc.TelegramBotToken = nil
	nc.WebhookURL = nil
}
