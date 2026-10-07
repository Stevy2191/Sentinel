// Package stream is Sentinel's live push channel: a Server-Sent Events
// writer and an in-memory hub that fans messages out to subscribers by key.
// The network tools stream run events through it; network phase 6 reuses it
// for live maps.
package stream

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"time"
)

// KeepAlive is how often a quiet stream sends a comment, so proxies and
// browsers do not close it as idle.
const KeepAlive = 15 * time.Second

// Message is one SSE frame. Data is already-encoded JSON.
type Message struct {
	ID    string // "" = no id line
	Event string // "" = the default "message" event
	Data  []byte
}

// Writer writes SSE frames to one response, flushing each.
type Writer struct {
	w http.ResponseWriter
	f http.Flusher
}

// ErrNoFlush is returned by NewWriter for a response that cannot flush, which
// would buffer the whole stream.
var ErrNoFlush = errors.New("stream: the response writer cannot flush")

// NewWriter starts an event stream: it sets the SSE headers (including
// X-Accel-Buffering: no, so nginx passes frames through unbuffered), writes
// 200 and flushes.
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrNoFlush
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &Writer{w: w, f: f}, nil
}

// Send writes one frame: an id line and an event line when set, one data
// line per line of Data, then the blank line that ends the frame.
func (w *Writer) Send(m Message) error {
	var b bytes.Buffer
	if m.ID != "" {
		b.WriteString("id: " + m.ID + "\n")
	}
	if m.Event != "" {
		b.WriteString("event: " + m.Event + "\n")
	}
	for _, line := range strings.Split(string(m.Data), "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	return w.write(b.Bytes())
}

// Comment writes a comment frame (": text"), which clients ignore; used as a
// keep-alive.
func (w *Writer) Comment(text string) error {
	return w.write([]byte(": " + text + "\n\n"))
}

func (w *Writer) write(b []byte) error {
	if _, err := w.w.Write(b); err != nil {
		return err
	}
	w.f.Flush()
	return nil
}
