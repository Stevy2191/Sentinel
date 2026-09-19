package models

import (
	"testing"
	"time"
)

func TestParseReportTimezone(t *testing.T) {
	for _, name := range []string{"UTC", "America/Chicago", "Europe/London", "Asia/Tokyo"} {
		if _, err := ParseReportTimezone(name); err != nil {
			t.Errorf("ParseReportTimezone(%q) = %v, want ok", name, err)
		}
	}
	// Whitespace is trimmed rather than rejected: it comes from a text input.
	if _, err := ParseReportTimezone("  America/Chicago  "); err != nil {
		t.Errorf("padded zone rejected: %v", err)
	}
}

func TestParseReportTimezone_Rejects(t *testing.T) {
	for _, name := range []string{"", "   ", "Mars/Olympus", "CDT", "GMT+5"} {
		if _, err := ParseReportTimezone(name); err == nil {
			t.Errorf("ParseReportTimezone(%q) succeeded, want an error", name)
		}
	}
}

// "Local" is the server process's zone — the accidental behaviour this setting
// exists to replace, so naming it explicitly must not be accepted.
func TestParseReportTimezone_RejectsLocal(t *testing.T) {
	for _, name := range []string{"Local", "local", "LOCAL"} {
		if _, err := ParseReportTimezone(name); err == nil {
			t.Errorf("ParseReportTimezone(%q) succeeded, want an error", name)
		}
	}
}

// The whole point: a timestamp must render in the configured zone, not UTC.
func TestParseReportTimezone_ShiftsTheClock(t *testing.T) {
	loc, err := ParseReportTimezone("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	utc := time.Date(2026, 9, 19, 2, 30, 0, 0, time.UTC)
	got := utc.In(loc).Format("January 02, 2006 at 15:04 MST")
	want := "September 18, 2026 at 21:30 CDT"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
