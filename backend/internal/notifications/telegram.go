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
	"strconv"
	"strings"
	"time"
)

const (
	defaultTelegramTimeout = 30 * time.Second
	defaultTelegramAPIBase = "https://api.telegram.org"
)

// TelegramPlugin delivers notifications via the Telegram Bot API.
type TelegramPlugin struct {
	botToken   string
	chatID     string
	apiBase    string // API base URL; overridable in tests
	httpClient *http.Client
	logger     *log.Logger
}

// telegramPayload is the sendMessage request body.
type telegramPayload struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview,omitempty"`
}

// telegramResponse captures the parts of the API response we care about.
type telegramResponse struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
}

// NewTelegramPlugin builds a TelegramPlugin from TELEGRAM_* environment
// variables. Both TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID are required.
func NewTelegramPlugin() (*TelegramPlugin, error) {
	botToken := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if botToken == "" {
		return nil, errors.New("TELEGRAM_BOT_TOKEN is required")
	}
	chatID := strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID"))
	if chatID == "" {
		return nil, errors.New("TELEGRAM_CHAT_ID is required")
	}

	p := &TelegramPlugin{
		botToken:   botToken,
		chatID:     chatID,
		apiBase:    defaultTelegramAPIBase,
		httpClient: &http.Client{Timeout: defaultTelegramTimeout},
		logger:     log.Default(),
	}
	p.logger.Printf("[telegram] Telegram plugin initialized for chat %s", chatID)
	return p, nil
}

// Name returns the plugin's channel name.
func (p *TelegramPlugin) Name() string { return "telegram" }

// IsEnabled reports whether both the bot token and chat ID are configured.
func (p *TelegramPlugin) IsEnabled() bool { return p.botToken != "" && p.chatID != "" }

// ValidateConfig validates the given config map, or the plugin's own config when
// config is nil (used by NotificationManager at registration).
func (p *TelegramPlugin) ValidateConfig(config map[string]interface{}) error {
	if config == nil {
		config = map[string]interface{}{"botToken": p.botToken, "chatId": p.chatID}
	}

	botToken, _ := config["botToken"].(string)
	if strings.TrimSpace(botToken) == "" {
		return errors.New("botToken is required")
	}

	chatID, _ := config["chatId"].(string)
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return errors.New("chatId is required")
	}
	// Chat IDs are integers (negative for groups/channels); usernames like
	// "@channel" are also accepted by Telegram.
	if !strings.HasPrefix(chatID, "@") {
		if _, err := strconv.ParseInt(chatID, 10, 64); err != nil {
			return fmt.Errorf("chatId must be a numeric id or @username, got %q", chatID)
		}
	}
	return nil
}

// endpoint returns the sendMessage API URL for the configured bot. The bot
// token contains a ':' which is a valid path character and is preserved.
func (p *TelegramPlugin) endpoint() string {
	base := p.apiBase
	if base == "" {
		base = defaultTelegramAPIBase
	}
	u, err := url.Parse(base)
	if err != nil {
		// Fall back to string construction; deliver will surface any error.
		return strings.TrimRight(base, "/") + "/bot" + p.botToken + "/sendMessage"
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/bot" + p.botToken + "/sendMessage"
	return u.String()
}

// Send posts the notification to Telegram, retrying once on network, 429, or 5xx
// errors. It respects the context deadline (default 30s).
func (p *TelegramPlugin) Send(ctx context.Context, message *NotificationMessage) error {
	if message == nil {
		return errors.New("message is nil")
	}
	if message.MonitorName == "" || message.Status == "" {
		return errors.New("message MonitorName and Status are required")
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultTelegramTimeout)
		defer cancel()
	}

	payload, err := json.Marshal(telegramPayload{
		ChatID:                p.chatID,
		Text:                  p.buildText(message),
		ParseMode:             "MarkdownV2",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("marshaling telegram payload: %w", err)
	}

	start := time.Now()
	if err := p.sendWithRetry(ctx, payload); err != nil {
		p.logger.Printf("[telegram] ❌ Telegram failed: %v", err)
		return err
	}
	p.logger.Printf("[telegram] ✅ Telegram sent to %s (%dms)", p.chatID, time.Since(start).Milliseconds())
	return nil
}

// sendWithRetry attempts delivery, retrying once after 2s on retriable errors.
func (p *TelegramPlugin) sendWithRetry(ctx context.Context, payload []byte) error {
	const maxRetries = 1

	var err error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			p.logger.Printf("[telegram] retry %d/%d after 2s", attempt, maxRetries)
			select {
			case <-ctx.Done():
				return fmt.Errorf("telegram send cancelled: %w", ctx.Err())
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

// deliver performs a single sendMessage call. 4xx (except 429) are nonRetriable;
// network errors, 429, and 5xx are retriable.
func (p *TelegramPlugin) deliver(ctx context.Context, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return nonRetriable{fmt.Errorf("building telegram request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	var apiResp telegramResponse
	_ = json.Unmarshal(body, &apiResp)
	desc := strings.TrimSpace(apiResp.Description)
	if desc == "" {
		desc = strings.TrimSpace(string(body))
	}

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode <= 299 && apiResp.OK:
		return nil
	case resp.StatusCode >= 200 && resp.StatusCode <= 299:
		// 2xx but ok=false is a definitive API rejection; do not retry.
		return nonRetriable{fmt.Errorf("telegram API error: %s", desc)}
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("telegram rate limited (429): %s", desc)
	case resp.StatusCode >= 400 && resp.StatusCode <= 499:
		return nonRetriable{fmt.Errorf("telegram client error %d: %s", resp.StatusCode, desc)}
	default:
		return fmt.Errorf("telegram server error %d: %s", resp.StatusCode, desc)
	}
}

// buildText renders the MarkdownV2 message body, escaping all dynamic content.
func (p *TelegramPlugin) buildText(m *NotificationMessage) string {
	emoji := "🟢"
	if m.Status == "down" {
		emoji = "🔴"
	}
	base := baseURL()

	var b strings.Builder
	// Header: *Name* - <emoji> STATUS  (the literal hyphen must be escaped)
	fmt.Fprintf(&b, "*%s* \\- %s *%s*\n\n", escapeMDV2(m.MonitorName), emoji, escapeMDV2(strings.ToUpper(m.Status)))

	fmt.Fprintf(&b, "*Status:* %s\n", escapeMDV2(m.Status))
	if m.PreviousStatus != "" && m.PreviousStatus != m.Status {
		fmt.Fprintf(&b, "*Previous:* %s\n", escapeMDV2(m.PreviousStatus))
	}
	if m.MonitorURL != "" {
		fmt.Fprintf(&b, "*URL:* `%s`\n", escapeMDV2Code(m.MonitorURL))
	}
	if m.ResponseTimeMs > 0 {
		fmt.Fprintf(&b, "*Response time:* %s\n", escapeMDV2(strconv.Itoa(m.ResponseTimeMs)+"ms"))
	}
	if m.Status == "recovered" && m.DowntimeDuration > 0 {
		fmt.Fprintf(&b, "*Downtime:* %s\n", escapeMDV2(m.DowntimeDuration.String()))
	}
	if !m.Timestamp.IsZero() {
		fmt.Fprintf(&b, "*Time:* %s\n", escapeMDV2(m.Timestamp.Format("Mon, 02 Jan 2006 15:04:05 MST")))
	}
	if m.Message != "" {
		fmt.Fprintf(&b, "\n%s\n", escapeMDV2(m.Message))
	}

	detailURL := fmt.Sprintf("%s/monitors/%s", base, m.MonitorID)
	reportURL := fmt.Sprintf("%s/reports?monitor_id=%s", base, m.MonitorID)
	fmt.Fprintf(&b, "\n[View in Sentinel](%s) \\• [View Report](%s)", escapeMDV2URL(detailURL), escapeMDV2URL(reportURL))

	return b.String()
}

// escapeMDV2 escapes all MarkdownV2 reserved characters in ordinary text.
func escapeMDV2(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	for _, c := range []string{"_", "*", "[", "]", "(", ")", "~", "`", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"} {
		s = strings.ReplaceAll(s, c, "\\"+c)
	}
	return s
}

// escapeMDV2Code escapes the characters reserved inside a MarkdownV2 code span.
func escapeMDV2Code(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "`", "\\`")
}

// escapeMDV2URL escapes the characters reserved inside a MarkdownV2 link target.
func escapeMDV2URL(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, ")", "\\)")
}
