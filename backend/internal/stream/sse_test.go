package stream

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewWriterHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	if _, err := NewWriter(rec); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !rec.Flushed {
		t.Errorf("code %d, flushed %v: want 200, flushed", rec.Code, rec.Flushed)
	}
	for k, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestSendFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	w, err := NewWriter(rec)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		send func() error
		want string
	}{
		{func() error { return w.Send(Message{ID: "7", Event: "event", Data: []byte(`{"seq":7}`)}) },
			"id: 7\nevent: event\ndata: {\"seq\":7}\n\n"},
		{func() error { return w.Send(Message{Data: []byte("a\nb")}) },
			"data: a\ndata: b\n\n"},
		{func() error { return w.Send(Message{Event: "end"}) },
			"event: end\ndata: \n\n"},
		{func() error { return w.Comment("keep-alive") },
			": keep-alive\n\n"},
	}
	for i, s := range steps {
		rec.Body.Reset()
		rec.Flushed = false
		if err := s.send(); err != nil {
			t.Fatal(err)
		}
		if got := rec.Body.String(); got != s.want || !rec.Flushed {
			t.Errorf("step %d wrote %q (flushed %v), want %q flushed", i, got, rec.Flushed, s.want)
		}
	}
}

// noFlush is a ResponseWriter that cannot flush.
type noFlush struct{ http.ResponseWriter }

func TestNewWriterNeedsFlusher(t *testing.T) {
	if _, err := NewWriter(noFlush{httptest.NewRecorder()}); !errors.Is(err, ErrNoFlush) {
		t.Errorf("err = %v, want ErrNoFlush", err)
	}
}
