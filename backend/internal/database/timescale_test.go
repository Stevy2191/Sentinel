package database

import (
	"strings"
	"testing"
)

func TestTimescaleMissingError(t *testing.T) {
	if err := TimescaleMissingError(TimescaleStatus{Available: true, Preloaded: true}); err != nil {
		t.Fatalf("available and preloaded: got %v, want nil", err)
	}

	err := TimescaleMissingError(TimescaleStatus{})
	if err == nil {
		t.Fatal("unavailable: got nil, want an error")
	}
	// The message is what an operator sees when the backend refuses to start,
	// so it must say what is wrong and what to do, not just fail.
	for _, want := range []string{"TimescaleDB", "docker-compose.yml", "timescale/timescaledb", "docker compose up -d", "GETTING_STARTED.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}

// The likeliest partial upgrade: the image line was copied into
// docker-compose.yml but the command block was not. The extension is
// installed but not preloaded, and CREATE EXTENSION would fail with a raw
// "must be preloaded" error, so the preflight must catch it and point at the
// command lines.
func TestTimescaleNotPreloaded(t *testing.T) {
	err := TimescaleMissingError(TimescaleStatus{Available: true, Preloaded: false})
	if err == nil {
		t.Fatal("available but not preloaded: got nil, want an error")
	}
	for _, want := range []string{"shared_preload_libraries=timescaledb", "command", "docker-compose.yml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not mention %q", err.Error(), want)
		}
	}
}
