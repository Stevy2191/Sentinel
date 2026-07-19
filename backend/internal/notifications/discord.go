package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const defaultDiscordTimeout = 30 * time.Second

// Discord embed colors (decimal RGB).
const (
	colorDiscordDown = 0xEF4444 // red
	colorDiscordUp   = 0x10B981 // emerald
)

// DiscordPlugin delivers notifications to a Discord webhook using embeds.
type DiscordPlugin struct {
	webhookURL string
	httpClient *http.Client
	logger     *log.Logger
}

// Discord webhook payload types.
type discordPayload struct {
	Username string         `json:"username,omitempty"`
	Content  string         `json:"content,omitempty"`
	Embeds   []discordEmbed `json:"embeds"`
}

type discordEmbed struct {
	Title       string              `json:"title,omitempty"`
	Description string              `json:"description,omitempty"`
	URL         string              `json:"url,omitempty"`
	Color       int                 `json:"color"`
	Fields      []discordEmbedField `json:"fields,omitempty"`
	Timestamp   string              `json:"timestamp,omitempty"`
	Footer      *discordEmbedFooter `json:"footer,omitempty"`
}

type discordEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type discordEmbedFooter struct {
	Text string `json:"text"`
}

// NewDiscordPlugin builds a DiscordPlugin from the DISCORD_WEBHOOK_URL
// environment variable.
func NewDiscordPlugin() (*DiscordPlugin, error) {
	webhookURL := strings.TrimSpace(os.Getenv("DISCORD_WEBHOOK_URL"))
	if webhookURL == "" {
		return nil, errors.New("DISCORD_WEBHOOK_URL is required")
	}
	u, err := url.ParseRequestURI(webhookURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("DISCORD_WEBHOOK_URL is not a valid https URL: %q", webhookURL)
	}

	p := &DiscordPlugin{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: defaultDiscordTimeout},
		logger:     log.Default(),
	}
	p.logger.Printf("[discord] Discord plugin initialized")
	return p, nil
}

// Name returns the plugin's channel name.
func (p *DiscordPlugin) Name() string { return "discord" }

// IsEnabled reports whether a webhook URL is configured.
func (p *DiscordPlugin) IsEnabled() bool { return p.webhookURL != "" }

// ValidateConfig validates the given config map, or the plugin's own config when
// config is nil (used by NotificationManager at registration).
func (p *DiscordPlugin) ValidateConfig(config map[string]interface{}) error {
	if config == nil {
		config = map[string]interface{}{"webhookUrl": p.webhookURL}
	}

	webhookURL, _ := config["webhookUrl"].(string)
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return errors.New("webhookUrl is required")
	}
	if !strings.HasPrefix(webhookURL, "https://") || !strings.Contains(webhookURL, "discord.com") {
		return fmt.Errorf("webhookUrl must be an https://discord.com webhook URL: %q", webhookURL)
	}
	return nil
}

// Send posts the embed message to the Discord webhook, retrying once on network,
// 429, or 5xx errors. It respects the context deadline (default 30s).
func (p *DiscordPlugin) Send(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}
	if message.MonitorName == "" || message.Status == "" {
		return errors.New("message MonitorName and Status are required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultDiscordTimeout)
		defer cancel()
	}

	payload, err := json.Marshal(p.buildPayload(message))
	if err != nil {
		return fmt.Errorf("marshaling discord payload: %w", err)
	}

	start := time.Now()
	if err := p.sendWithRetry(ctx, payload); err != nil {
		p.logger.Printf("[discord] ❌ Discord failed: %v", err)
		return err
	}
	p.logger.Printf("[discord] ✅ Discord sent (%dms)", time.Since(start).Milliseconds())
	return nil
}

// sendWithRetry attempts delivery, retrying once after 2s on retriable errors.
func (p *DiscordPlugin) sendWithRetry(ctx context.Context, payload []byte) error {
	const maxRetries = 1

	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.logger.Printf("[discord] retry %d/%d after 2s", attempt, maxRetries)
			select {
			case <-ctx.Done():
				return fmt.Errorf("discord send cancelled: %w", ctx.Err())
			case <-time.After(2 * time.Second):
			}
		}

		err = p.deliver(ctx, payload)
		if err == nil {
			return nil
		}
		var nr nonRetriable
		if errors.As(err, &nr) {
			return err
		}
	}
	return err
}

// deliver performs a single POST to the webhook. 4xx (except 429) are returned
// as nonRetriable; network errors, 429, and 5xx are retriable.
func (p *DiscordPlugin) deliver(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return nonRetriable{fmt.Errorf("building discord request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("discord rate limited (429): %s", strings.TrimSpace(string(body)))
	case resp.StatusCode >= 400 && resp.StatusCode <= 499:
		return nonRetriable{fmt.Errorf("discord client error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	default:
		return fmt.Errorf("discord server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// buildPayload constructs the Discord embed message.
func (p *DiscordPlugin) buildPayload(m *NotificationMessage) discordPayload {
	emoji := "🟢"
	color := colorDiscordUp
	if m.Status == "down" {
		emoji = "🔴"
		color = colorDiscordDown
	}

	fields := []discordEmbedField{
		{Name: "Status", Value: fmt.Sprintf("%s %s", emoji, m.Status), Inline: true},
	}
	if m.PreviousStatus != "" && m.PreviousStatus != m.Status {
		fields = append(fields, discordEmbedField{Name: "Previous Status", Value: m.PreviousStatus, Inline: true})
	}
	if m.ResponseTimeMs > 0 {
		fields = append(fields, discordEmbedField{Name: "Response Time", Value: fmt.Sprintf("%dms", m.ResponseTimeMs), Inline: true})
	}
	if m.Status == "recovered" && m.DowntimeDuration > 0 {
		fields = append(fields, discordEmbedField{Name: "Downtime Duration", Value: m.DowntimeDuration.String(), Inline: true})
	}

	base := baseURL()
	var desc strings.Builder
	if m.Message != "" {
		fmt.Fprintf(&desc, "%s\n", m.Message)
	}
	if m.MonitorURL != "" {
		fmt.Fprintf(&desc, "[%s](%s)\n", m.MonitorURL, m.MonitorURL)
	}
	// Discord webhook embeds cannot render interactive buttons; use markdown links.
	fmt.Fprintf(&desc, "\n[View in Sentinel](%s/monitors/%s) • [View Report](%s/reports?monitor_id=%s)",
		base, m.MonitorID, base, m.MonitorID)

	embed := discordEmbed{
		Title:       m.MonitorName,
		Description: desc.String(),
		URL:         fmt.Sprintf("%s/monitors/%s", base, m.MonitorID),
		Color:       color,
		Fields:      fields,
		Footer:      &discordEmbedFooter{Text: "Sentinel Monitoring"},
	}
	if !m.Timestamp.IsZero() {
		embed.Timestamp = m.Timestamp.UTC().Format(time.RFC3339)
	}

	return discordPayload{
		Username: "Sentinel",
		Content:  fmt.Sprintf("%s - %s", m.MonitorName, m.Status),
		Embeds:   []discordEmbed{embed},
	}
}
