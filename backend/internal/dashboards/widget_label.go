package dashboards

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxLabelText = 200

type labelConfig struct {
	Text string `json:"text"`
	Size string `json:"size"`
}

// LabelData is a label's response: its text and size (s, m or l).
type LabelData struct {
	Text string `json:"text"`
	Size string `json:"size"`
}

// labelWidget is plain text, e.g. a heading on a wall display. Its text is
// rendered as text, never as HTML or Markdown.
type labelWidget struct{}

func (labelWidget) Type() string { return "label" }

func (labelWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var c labelConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	c.Text = strings.TrimSpace(c.Text)
	switch {
	case c.Text == "":
		return nil, fieldErr("text", "is required")
	case utf8.RuneCountInString(c.Text) > maxLabelText:
		return nil, fieldErr("text", "must be 200 characters or fewer")
	case strings.ContainsFunc(c.Text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }):
		return nil, fieldErr("text", "must be plain text")
	}
	switch c.Size {
	case "":
		c.Size = "m"
	case "s", "m", "l":
	default:
		return nil, fieldErr("size", "must be s, m or l")
	}
	return json.Marshal(c)
}

func (labelWidget) Subjects(json.RawMessage) Subjects { return Subjects{} }

func (labelWidget) Resolve(_ context.Context, raw json.RawMessage, _ ResolveInput) (any, error) {
	var c labelConfig
	_ = json.Unmarshal(raw, &c)
	return LabelData{Text: c.Text, Size: c.Size}, nil
}

func (labelWidget) Refresh(json.RawMessage, string) time.Duration { return 0 }
