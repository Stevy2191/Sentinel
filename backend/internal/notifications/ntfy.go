package notifications

import (
	"bytes"
	"context"
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

const (
	defaultNtfyURL     = "https://ntfy.sh"
	defaultNtfyTimeout = 30 * time.Second
	slowResponseMs     = 1000 // responses at/above this add an "hourglass" tag
)

// NtfyPlugin delivers notifications to an ntfy topic via HTTP.
type NtfyPlugin struct {
	url        string
	topic      string
	httpClient *http.Client
	logger     *log.Logger
}

// NewNtfyPlugin builds an NtfyPlugin from NTFY_* environment variables.
// NTFY_TOPIC is required; NTFY_URL defaults to https://ntfy.sh.
func NewNtfyPlugin() (*NtfyPlugin, error) {
	serverURL := strings.TrimSpace(os.Getenv("NTFY_URL"))
	if serverURL == "" {
		serverURL = defaultNtfyURL
	}
	serverURL = strings.TrimRight(serverURL, "/")

	topic := strings.TrimSpace(os.Getenv("NTFY_TOPIC"))
	if topic == "" {
		return nil, errors.New("NTFY_TOPIC is required")
	}

	p := &NtfyPlugin{
		url:        serverURL,
		topic:      topic,
		httpClient: &http.Client{Timeout: defaultNtfyTimeout},
		logger:     log.Default(),
	}
	p.logger.Printf("[ntfy] Ntfy plugin initialized for topic %s", topic)
	return p, nil
}

// Name returns the plugin's channel name.
func (p *NtfyPlugin) Name() string { return "ntfy" }

// IsEnabled reports whether a topic is configured (URL always has a default).
func (p *NtfyPlugin) IsEnabled() bool { return p.topic != "" }

// ValidateConfig validates the given config map, or the plugin's own config when
// config is nil (used by NotificationManager at registration).
func (p *NtfyPlugin) ValidateConfig(config map[string]interface{}) error {
	if config == nil {
		config = map[string]interface{}{"topic": p.topic, "url": p.url}
	}

	if topic, _ := config["topic"].(string); strings.TrimSpace(topic) == "" {
		return errors.New("topic is required")
	}

	if raw, ok := config["url"].(string); ok && strings.TrimSpace(raw) != "" {
		u, err := url.ParseRequestURI(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("url is not a valid http(s) URL: %q", raw)
		}
	}

	return nil
}

// Send publishes the notification to the configured ntfy topic, retrying once on
// network/5xx errors. It respects the context deadline (defaulting to 30s).
func (p *NtfyPlugin) Send(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}
	if message.MonitorName == "" || message.Status == "" {
		return errors.New("message MonitorName and Status are required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultNtfyTimeout)
		defer cancel()
	}

	title := p.buildTitle(message)
	body := fmt.Sprintf("%s\n\n%s", title, p.buildBody(message))
	priority := "default"
	if message.Status == "down" {
		priority = "high"
	}
	tags := p.buildTags(message)
	click := fmt.Sprintf("%s/monitors/%s", baseURL(), message.MonitorID)

	start := time.Now()
	if err := p.sendWithRetry(ctx, body, message.MonitorName, priority, tags, click); err != nil {
		p.logger.Printf("[ntfy] ❌ Ntfy failed: %v", err)
		return err
	}
	p.logger.Printf("[ntfy] ✅ Ntfy sent to %s (%dms)", p.topic, time.Since(start).Milliseconds())
	return nil
}

// sendWithRetry attempts publication, retrying once after 2s on retriable
// (network or 5xx) errors. 4xx client errors are not retried.
func (p *NtfyPlugin) sendWithRetry(ctx context.Context, body, title, priority, tags, click string) error {
	const maxRetries = 1

	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.logger.Printf("[ntfy] retry %d/%d after 2s", attempt, maxRetries)
			select {
			case <-ctx.Done():
				return fmt.Errorf("ntfy send cancelled: %w", ctx.Err())
			case <-time.After(2 * time.Second):
			}
		}

		err = p.deliver(ctx, body, title, priority, tags, click)
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

// deliver performs a single POST to {url}/{topic}. A 4xx response is returned as
// a nonRetriable error; network errors and 5xx are retriable.
func (p *NtfyPlugin) deliver(ctx context.Context, body, title, priority, tags, click string) error {
	endpoint := fmt.Sprintf("%s/%s", p.url, p.topic)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader([]byte(body)))
	if err != nil {
		return nonRetriable{fmt.Errorf("building ntfy request: %w", err)}
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	if tags != "" {
		req.Header.Set("Tags", tags)
	}
	if click != "" {
		req.Header.Set("Click", click)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode <= 499:
		return nonRetriable{fmt.Errorf("ntfy client error %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))}
	default:
		return fmt.Errorf("ntfy server error %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
}

// buildTitle constructs the notification title for the message's status.
func (p *NtfyPlugin) buildTitle(m *NotificationMessage) string {
	switch m.Status {
	case "down":
		return fmt.Sprintf("[ALERT] %s went DOWN", m.MonitorName)
	case "up":
		return fmt.Sprintf("[RECOVERED] %s is UP", m.MonitorName)
	case "recovered":
		return fmt.Sprintf("[RECOVERED] %s recovered", m.MonitorName)
	default:
		return fmt.Sprintf("[Sentinel] %s status: %s", m.MonitorName, m.Status)
	}
}

// buildBody assembles the human-readable notification details.
func (p *NtfyPlugin) buildBody(m *NotificationMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Status: %s", m.Status)
	if m.PreviousStatus != "" {
		fmt.Fprintf(&b, "\nPrevious: %s", m.PreviousStatus)
	}
	if m.MonitorURL != "" {
		fmt.Fprintf(&b, "\nURL: %s", m.MonitorURL)
	}
	if m.ResponseTimeMs > 0 {
		fmt.Fprintf(&b, "\nResponse time: %dms", m.ResponseTimeMs)
	}
	if m.Status == "recovered" && m.DowntimeDuration > 0 {
		fmt.Fprintf(&b, "\nDowntime: %s", m.DowntimeDuration.String())
	}
	if !m.Timestamp.IsZero() {
		fmt.Fprintf(&b, "\nTime: %s", m.Timestamp.Format("Mon, 02 Jan 2006 15:04:05 MST"))
	}
	if m.Message != "" {
		fmt.Fprintf(&b, "\n\n%s", m.Message)
	}
	return b.String()
}

// buildTags selects ntfy emoji tags for the message.
func (p *NtfyPlugin) buildTags(m *NotificationMessage) string {
	var tags []string
	switch m.Status {
	case "down":
		tags = append(tags, "red_circle")
	default:
		tags = append(tags, "green_circle")
	}
	if m.ResponseTimeMs >= slowResponseMs {
		tags = append(tags, "hourglass")
	}
	return strings.Join(tags, ",")
}
