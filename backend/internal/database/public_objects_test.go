package database

import (
	"strings"
	"testing"
)

func TestNonTableObjectsWarning(t *testing.T) {
	if got := NonTableObjectsWarning(nil); got != "" {
		t.Errorf("no objects: got %q, want no warning", got)
	}

	got := NonTableObjectsWarning([]string{"function public.touch_updated_at", "view public.monitor_summary"})
	// The warning must name what will be lost and why, so whoever added it
	// knows to move it or change the backup.
	for _, want := range []string{"public.touch_updated_at", "public.monitor_summary", "backup", "BackupService.dumpArgs"} {
		if !strings.Contains(got, want) {
			t.Errorf("warning %q does not mention %q", got, want)
		}
	}
}
