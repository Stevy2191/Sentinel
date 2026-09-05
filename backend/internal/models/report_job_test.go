package models

import "testing"

func TestReportJobIsTerminal(t *testing.T) {
	cases := map[string]bool{
		JobQueued:    false,
		JobRunning:   false,
		JobSucceeded: true,
		JobFailed:    true,
	}
	for status, want := range cases {
		j := &ReportJob{Status: status}
		if got := j.IsTerminal(); got != want {
			t.Errorf("status %q: IsTerminal = %v, want %v", status, got, want)
		}
	}
}
