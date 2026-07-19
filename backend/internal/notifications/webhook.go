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

const defaultWebhookTimeout = 30 * time.Second

// WebhookPlugin delivers notifications as a JSON POST to a user-defined HTTP
// endpoint.
type WebhookPlugin struct {
	webhookURL string
	httpClient *http.Client
	logger     *log.Logger
}

// Webhook payload types. The shape is a stable, documented contract for user
// integrations, so field names use snake_case and optional sections are omitted
// when empty.
type webhookPayload struct {
	Type     string           `json:"type"`
	Version  string           `json:"version"`
	Monitor  webhookMonitor   `json:"monitor"`
	Alert    webhookAlert     `json:"alert"`
	Metrics  webhookMetrics   `json:"metrics"`
	Incident *webhookIncident `json:"incident,omitempty"`
	Links    webhookLinks     `json:"links"`
}

type webhookMonitor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type,omitempty"`
}

type webhookAlert struct {
	Status         string `json:"status"`
	PreviousStatus string `json:"previous_status,omitempty"`
	Timestamp      string `json:"timestamp"`
	Message        string `json:"message,omitempty"`
}

type webhookMetrics struct {
	ResponseTimeMs          int `json:"response_time_ms"`
	DowntimeDurationSeconds int `json:"downtime_duration_seconds,omitempty"`
}

type webhookIncident struct {
	ID              string `json:"id"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
}

type webhookLinks struct {
	ViewInSentinel string `json:"view_in_sentinel"`
	ViewReport     string `json:"view_report"`
}

// NewWebhookPlugin builds a WebhookPlugin from the WEBHOOK_URL environment
// variable.
func NewWebhookPlugin() (*WebhookPlugin, error) {
	webhookURL := strings.TrimSpace(os.Getenv("WEBHOOK_URL"))
	if webhookURL == "" {
		return nil, errors.New("WEBHOOK_URL is required")
	}
	u, err := url.ParseRequestURI(webhookURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("WEBHOOK_URL is not a valid http(s) URL: %q", webhookURL)
	}

	p := &WebhookPlugin{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: defaultWebhookTimeout},
		logger:     log.Default(),
	}
	p.logger.Printf("[webhook] Webhook plugin initialized for %s", webhookURL)
	return p, nil
}

// Name returns the plugin's channel name.
func (p *WebhookPlugin) Name() string { return "webhook" }

// IsEnabled reports whether a webhook URL is configured.
func (p *WebhookPlugin) IsEnabled() bool { return p.webhookURL != "" }

// ValidateConfig validates the given config map, or the plugin's own config when
// config is nil (used by NotificationManager at registration).
func (p *WebhookPlugin) ValidateConfig(config map[string]interface{}) error {
	if config == nil {
		config = map[string]interface{}{"webhookUrl": p.webhookURL}
	}

	webhookURL, _ := config["webhookUrl"].(string)
	webhookURL = strings.TrimSpace(webhookURL)
	if webhookURL == "" {
		return errors.New("webhookUrl is required")
	}
	if !strings.HasPrefix(webhookURL, "http://") && !strings.HasPrefix(webhookURL, "https://") {
		return fmt.Errorf("webhookUrl must start with http:// or https://, got %q", webhookURL)
	}
	if _, err := url.ParseRequestURI(webhookURL); err != nil {
		return fmt.Errorf("webhookUrl is not a valid URL: %w", err)
	}
	return nil
}

// Send posts the JSON payload to the configured endpoint, retrying once on
// network, 429, or 5xx errors. It respects the context deadline (default 30s).
func (p *WebhookPlugin) Send(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}
	if message.MonitorName == "" || message.Status == "" {
		return errors.New("message MonitorName and Status are required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultWebhookTimeout)
		defer cancel()
	}

	payload, err := json.Marshal(p.buildPayload(message))
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	start := time.Now()
	if err := p.sendWithRetry(ctx, payload); err != nil {
		p.logger.Printf("[webhook] ❌ Webhook failed: %v", err)
		return err
	}
	p.logger.Printf("[webhook] ✅ Webhook sent to %s (%dms)", p.webhookURL, time.Since(start).Milliseconds())
	return nil
}

// sendWithRetry attempts delivery, retrying once after 2s on retriable errors.
func (p *WebhookPlugin) sendWithRetry(ctx context.Context, payload []byte) error {
	const maxRetries = 1

	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.logger.Printf("[webhook] retry %d/%d after 2s", attempt, maxRetries)
			select {
			case <-ctx.Done():
				return fmt.Errorf("webhook send cancelled: %w", ctx.Err())
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

// deliver performs a single POST. 4xx (except 429) are nonRetriable; network
// errors, 429, and 5xx are retriable.
func (p *WebhookPlugin) deliver(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return nonRetriable{fmt.Errorf("building webhook request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Sentinel/1.0")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("webhook rate limited (429): %s", strings.TrimSpace(string(body)))
	case resp.StatusCode >= 400 && resp.StatusCode <= 499:
		return nonRetriable{fmt.Errorf("webhook client error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))}
	default:
		return fmt.Errorf("webhook server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// buildPayload assembles the comprehensive JSON notification payload.
func (p *WebhookPlugin) buildPayload(m *NotificationMessage) webhookPayload {
	base := baseURL()

	timestamp := m.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	payload := webhookPayload{
		Type:    "sentinel_alert",
		Version: "1.0",
		Monitor: webhookMonitor{
			ID:   m.MonitorID.String(),
			Name: m.MonitorName,
			URL:  m.MonitorURL,
		},
		Alert: webhookAlert{
			Status:         m.Status,
			PreviousStatus: m.PreviousStatus,
			Timestamp:      timestamp.UTC().Format(time.RFC3339),
			Message:        m.Message,
		},
		Metrics: webhookMetrics{
			ResponseTimeMs:          m.ResponseTimeMs,
			DowntimeDurationSeconds: int(m.DowntimeDuration.Seconds()),
		},
		Links: webhookLinks{
			ViewInSentinel: fmt.Sprintf("%s/monitors/%s", base, m.MonitorID),
			ViewReport:     fmt.Sprintf("%s/reports?monitor_id=%s", base, m.MonitorID),
		},
	}

	if m.IncidentID != nil {
		payload.Incident = &webhookIncident{
			ID:              m.IncidentID.String(),
			DurationSeconds: int(m.DowntimeDuration.Seconds()),
		}
	}

	return payload
}
