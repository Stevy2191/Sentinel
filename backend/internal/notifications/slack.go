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

const defaultSlackTimeout = 30 * time.Second

// SlackPlugin delivers notifications to a Slack incoming webhook using Block Kit.
type SlackPlugin struct {
	webhookURL string
	httpClient *http.Client
	logger     *log.Logger
}

// Slack Block Kit payload types.
type slackPayload struct {
	Text        string            `json:"text"`
	Attachments []slackAttachment `json:"attachments"`
}

type slackAttachment struct {
	Color  string       `json:"color,omitempty"`
	Blocks []slackBlock `json:"blocks"`
}

type slackBlock struct {
	Type     string         `json:"type"`
	Text     *slackText     `json:"text,omitempty"`
	Fields   []slackText    `json:"fields,omitempty"`
	Elements []slackElement `json:"elements,omitempty"`
}

type slackText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

type slackElement struct {
	Type string     `json:"type"`
	Text *slackText `json:"text,omitempty"`
	URL  string     `json:"url,omitempty"`
}

// NewSlackPlugin builds a SlackPlugin from the SLACK_WEBHOOK_URL environment
// variable.
func NewSlackPlugin() (*SlackPlugin, error) {
	webhookURL := strings.TrimSpace(os.Getenv("SLACK_WEBHOOK_URL"))
	if webhookURL == "" {
		return nil, errors.New("SLACK_WEBHOOK_URL is required")
	}
	u, err := url.ParseRequestURI(webhookURL)
	if err != nil || u.Scheme != "https" {
		return nil, fmt.Errorf("SLACK_WEBHOOK_URL is not a valid https URL: %q", webhookURL)
	}

	p := &SlackPlugin{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: defaultSlackTimeout},
		logger:     log.Default(),
	}
	p.logger.Printf("[slack] Slack plugin initialized")
	return p, nil
}

// Name returns the plugin's channel name.
func (p *SlackPlugin) Name() string { return "slack" }

// IsEnabled reports whether a webhook URL is configured.
func (p *SlackPlugin) IsEnabled() bool { return p.webhookURL != "" }

// ValidateConfig validates the given config map, or the plugin's own config when
// config is nil (used by NotificationManager at registration).
func (p *SlackPlugin) ValidateConfig(config map[string]interface{}) error {
	if config == nil {
		config = map[string]interface{}{"webhookUrl": p.webhookURL}
	}

	webhookURL, _ := config["webhookUrl"].(string)
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return errors.New("webhookUrl is required")
	}
	if !strings.HasPrefix(webhookURL, "https://") || !strings.Contains(webhookURL, "hooks.slack.com") {
		return fmt.Errorf("webhookUrl must be an https://hooks.slack.com URL: %q", webhookURL)
	}
	return nil
}

// Send posts the Block Kit message to the Slack webhook, retrying once on
// network, 429, or 5xx errors. It respects the context deadline (default 30s).
func (p *SlackPlugin) Send(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}
	if message.MonitorName == "" || message.Status == "" {
		return errors.New("message MonitorName and Status are required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultSlackTimeout)
		defer cancel()
	}

	payload, err := json.Marshal(p.buildPayload(message))
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	start := time.Now()
	if err := p.sendWithRetry(ctx, payload); err != nil {
		p.logger.Printf("[slack] ❌ Slack failed: %v", err)
		return err
	}
	p.logger.Printf("[slack] ✅ Slack sent (%dms)", time.Since(start).Milliseconds())
	return nil
}

// sendWithRetry attempts delivery, retrying once after 2s on retriable errors.
func (p *SlackPlugin) sendWithRetry(ctx context.Context, payload []byte) error {
	const maxRetries = 1

	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.logger.Printf("[slack] retry %d/%d after 2s", attempt, maxRetries)
			select {
			case <-ctx.Done():
				return fmt.Errorf("slack send cancelled: %w", ctx.Err())
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
func (p *SlackPlugin) deliver(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return nonRetriable{fmt.Errorf("building slack request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("slack rate limited (429): %s", strings.TrimSpace(string(body)))
	case resp.StatusCode >= 400 && resp.StatusCode <= 499:
		return nonRetriable{fmt.Errorf("slack client error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	default:
		return fmt.Errorf("slack server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// buildPayload constructs the Slack Block Kit message, wrapping the blocks in a
// colored attachment so Slack renders the status color bar.
func (p *SlackPlugin) buildPayload(m *NotificationMessage) slackPayload {
	emoji := "🟢"
	color := colorSuccess
	if m.Status == "down" {
		emoji = "🔴"
		color = colorError
	}

	fields := []slackText{
		{Type: "mrkdwn", Text: fmt.Sprintf("*Status*\n%s %s", emoji, m.Status)},
	}
	if m.PreviousStatus != "" && m.PreviousStatus != m.Status {
		fields = append(fields, slackText{Type: "mrkdwn", Text: fmt.Sprintf("*Previous Status*\n%s", m.PreviousStatus)})
	}
	if m.ResponseTimeMs > 0 {
		fields = append(fields, slackText{Type: "mrkdwn", Text: fmt.Sprintf("*Response Time*\n%dms", m.ResponseTimeMs)})
	}
	if !m.Timestamp.IsZero() {
		fields = append(fields, slackText{Type: "mrkdwn", Text: fmt.Sprintf("*Timestamp*\n%s", m.Timestamp.Format("Mon, 02 Jan 2006 15:04:05 MST"))})
	}
	if m.Status == "recovered" && m.DowntimeDuration > 0 {
		fields = append(fields, slackText{Type: "mrkdwn", Text: fmt.Sprintf("*Downtime Duration*\n%s", m.DowntimeDuration.String())})
	}

	detailText := ""
	if m.Message != "" {
		detailText = m.Message + "\n"
	}
	if m.MonitorURL != "" {
		detailText += fmt.Sprintf("<%s|%s>", m.MonitorURL, m.MonitorURL)
	}

	base := baseURL()
	blocks := []slackBlock{
		{
			Type: "header",
			Text: &slackText{Type: "plain_text", Text: m.MonitorName, Emoji: true},
		},
		{
			Type:   "section",
			Fields: fields,
		},
		{Type: "divider"},
	}
	if detailText != "" {
		blocks = append(blocks, slackBlock{
			Type: "section",
			Text: &slackText{Type: "mrkdwn", Text: detailText},
		})
	}
	blocks = append(blocks, slackBlock{
		Type: "actions",
		Elements: []slackElement{
			{Type: "button", Text: &slackText{Type: "plain_text", Text: "View in Sentinel"}, URL: fmt.Sprintf("%s/monitors/%s", base, m.MonitorID)},
			{Type: "button", Text: &slackText{Type: "plain_text", Text: "View Report"}, URL: fmt.Sprintf("%s/reports?monitor_id=%s", base, m.MonitorID)},
		},
	})

	return slackPayload{
		Text:        fmt.Sprintf("%s - %s", m.MonitorName, m.Status),
		Attachments: []slackAttachment{{Color: color, Blocks: blocks}},
	}
}
