package models

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/cryptutil"
)

// SMTP connection security modes for the email channel.
const (
	// SMTPSecurityNone sends over a plaintext connection. Usable only for an
	// internal relay that requires no credentials - see NotificationConfig.Validate.
	SMTPSecurityNone = "none"
	// SMTPSecuritySTARTTLS connects in plaintext and then requires an upgrade to
	// TLS. The upgrade is mandatory: a server that does not advertise STARTTLS is
	// an error, never a silent fallback to cleartext.
	SMTPSecuritySTARTTLS = "starttls"
	// SMTPSecuritySSLTLS performs a TLS handshake on connect (implicit TLS/SMTPS,
	// conventionally port 465).
	SMTPSecuritySSLTLS = "ssltls"
)

// ValidSMTPSecurity lists the accepted smtp_security values.
var ValidSMTPSecurity = map[string]bool{
	SMTPSecurityNone:     true,
	SMTPSecuritySTARTTLS: true,
	SMTPSecuritySSLTLS:   true,
}

// ResolveSMTPSecurity normalizes a stored smtp_security value. A nil or empty
// value means STARTTLS, which is what the email plugin did unconditionally before
// the mode became configurable.
func ResolveSMTPSecurity(v *string) string {
	if v == nil || *v == "" {
		return SMTPSecuritySTARTTLS
	}
	return *v
}

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
	// SMTPSecurity is the connection security mode: none, starttls, or ssltls.
	// Nil means starttls (see ResolveSMTPSecurity). Not a secret.
	SMTPSecurity *string `json:"smtp_security" gorm:"column:smtp_security"`
	// SMTPSkipTLSVerify disables certificate verification for self-signed
	// internal mail servers. Insecure, and an explicit opt-in.
	SMTPSkipTLSVerify bool `json:"smtp_skip_tls_verify" gorm:"column:smtp_skip_tls_verify"`

	// Slack/Discord/Webhook/Ntfy (generic URL)
	WebhookURL *string `json:"webhook_url,omitempty" gorm:"column:webhook_url"` // hidden in list, returned on single GET

	// Telegram
	TelegramBotToken *string `json:"telegram_bot_token,omitempty" gorm:"column:telegram_bot_token"` // never returned in list responses
	TelegramChatID   *string `json:"telegram_chat_id" gorm:"column:telegram_chat_id"`

	// Ntfy
	NtfyURL       *string `json:"ntfy_url" gorm:"column:ntfy_url"`
	NtfyTopic     *string `json:"ntfy_topic" gorm:"column:ntfy_topic"`
	NtfyAuthToken *string `json:"ntfy_auth_token,omitempty" gorm:"column:ntfy_auth_token"` // never returned in list responses

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

// secretFields lists the pointer fields that are encrypted at rest. Order
// doesn't matter; it's a shared list so BeforeSave/AfterSave/AfterFind stay
// in sync automatically if a field is ever added or removed.
func (nc *NotificationConfig) secretFields() []**string {
	return []**string{&nc.SMTPPassword, &nc.TelegramBotToken, &nc.WebhookURL, &nc.NtfyAuthToken}
}

// BeforeSave encrypts secret fields immediately before they're written,
// covering both Create and Update (Save calls one or the other).
func (nc *NotificationConfig) BeforeSave(tx *gorm.DB) error {
	for _, f := range nc.secretFields() {
		if *f == nil || **f == "" {
			continue
		}
		enc, err := cryptutil.Encrypt(**f)
		if err != nil {
			return fmt.Errorf("encrypting notification secret: %w", err)
		}
		*f = &enc
	}
	return nil
}

// AfterSave decrypts secret fields back in memory after a successful write,
// so the caller's in-memory struct (e.g. an API handler about to build a
// response) sees plaintext, not the ciphertext just persisted to Postgres.
func (nc *NotificationConfig) AfterSave(tx *gorm.DB) error {
	nc.decryptSecretsLenient()
	return nil
}

// AfterFind decrypts secret fields after every load (First/Find), covering
// NotificationConfigService and NotificationManager's direct queries alike.
func (nc *NotificationConfig) AfterFind(tx *gorm.DB) error {
	nc.decryptSecretsLenient()
	return nil
}

// decryptSecretsLenient decrypts in place, but treats a decryption failure as
// "this value predates encryption support" rather than a hard error: it
// leaves the field as-is (still plaintext) instead of failing the whole
// query. The value gets encrypted automatically the next time this channel
// is saved. This makes the rollout of encryption transparent for existing
// deployments with pre-existing plaintext rows - no separate migration step
// or backfill script is required.
func (nc *NotificationConfig) decryptSecretsLenient() {
	for _, f := range nc.secretFields() {
		if *f == nil || **f == "" {
			continue
		}
		dec, err := cryptutil.Decrypt(**f)
		if err != nil {
			log.Printf("[notification_config] value for a secret field could not be decrypted "+
				"(likely pre-encryption plaintext, will be encrypted on next save): %v", err)
			continue
		}
		*f = &dec
	}
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
		// Nil is allowed and means starttls; a present value must be recognized.
		if nc.SMTPSecurity != nil && !ValidSMTPSecurity[*nc.SMTPSecurity] {
			return errors.New("SMTP security must be one of: none, starttls, ssltls")
		}
		// "none" combined with a password is deliberately NOT rejected here. Go's
		// smtp.PlainAuth permits cleartext credentials to localhost, so a local
		// relay with a password is a legitimate config. The plugin, which knows the
		// host, is the authoritative check at send time.

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
		// The auth token is optional, but if the field is present it must not be blank.
		if nc.NtfyAuthToken != nil && *nc.NtfyAuthToken == "" {
			return errors.New("Ntfy auth token cannot be empty if provided")
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
	nc.NtfyAuthToken = nil
}
