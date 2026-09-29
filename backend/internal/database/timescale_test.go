package database

import (
	"strings"
	"testing"
)

func TestTimescaleMissingError(t *testing.T) {
	if err := TimescaleMissingError(true); err != nil {
		t.Fatalf("available: got %v, want nil", err)
	}

	err := TimescaleMissingError(false)
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
