package toolruns

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

func TestDBSettingsDefaultsAndRoundTrip(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	ss := services.NewSettingsService(db)

	got := LoadSettings(ctx, ss)
	if len(got.Allowlist) != 0 || got.Allowlist == nil || !got.ServerEnabled || got.RetentionDays != 30 {
		t.Fatalf("defaults = %+v, want an empty (non-nil) allowlist, server on, 30 days", got)
	}

	saved, err := SaveSettings(ctx, ss, Settings{
		Allowlist:     []string{" 10.0.0.0/24 ", "", "FileServer.Example.ORG", "10.0.0.0/24", "*.Lab.example.org"},
		ServerEnabled: false, RetentionDays: 7,
	})
	testdb.Must(t, err)
	want := Settings{Allowlist: []string{"10.0.0.0/24", "fileserver.example.org", "*.lab.example.org"}, ServerEnabled: false, RetentionDays: 7}
	if !reflect.DeepEqual(saved, want) {
		t.Errorf("saved = %+v, want %+v", saved, want)
	}
	if got := LoadSettings(ctx, ss); !reflect.DeepEqual(got, want) {
		t.Errorf("loaded = %+v, want %+v", got, want)
	}
	if raw := ss.GetString(ctx, models.SettingNetToolsAllowlist, ""); raw != `["10.0.0.0/24","fileserver.example.org","*.lab.example.org"]` {
		t.Errorf("stored allowlist = %s", raw)
	}
}

// One bad entry refuses the whole save, every bad entry is reported, and
// nothing is written.
func TestDBSettingsRefusesBadEntries(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	ss := services.NewSettingsService(db)
	_, err := SaveSettings(ctx, ss, Settings{
		Allowlist:     []string{"10.0.0.0/24", "10.0.0.0/7", "bad host!", "2001:db8::/32"},
		ServerEnabled: true, RetentionDays: 30,
	})
	var se *SettingsError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want *SettingsError", err)
	}
	if len(se.Entries) != 3 || se.Entries[0].Entry != "10.0.0.0/7" || se.Entries[1].Entry != "bad host!" ||
		se.Entries[2].Entry != "2001:db8::/32" {
		t.Fatalf("entries = %+v, want the /7, the bad host and the IPv6 CIDR, in order", se.Entries)
	}
	if se.Entries[0].Message != "too broad: use /8 or narrower" {
		t.Errorf("the /7's message = %q", se.Entries[0].Message)
	}
	if got := LoadSettings(ctx, ss); len(got.Allowlist) != 0 {
		t.Errorf("a refused save wrote %v", got.Allowlist)
	}
}

func TestDBSettingsRetentionBounds(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	ss := services.NewSettingsService(db)
	for _, days := range []int{0, 366} {
		_, err := SaveSettings(ctx, ss, Settings{RetentionDays: days, ServerEnabled: true})
		var se *SettingsError
		if !errors.As(err, &se) || len(se.Entries) != 0 || se.Message != "retention_days must be between 1 and 365" {
			t.Errorf("retention %d: err = %v, want a SettingsError with no entries", days, err)
		}
	}
	for _, days := range []int{1, 365} {
		got, err := SaveSettings(ctx, ss, Settings{RetentionDays: days, ServerEnabled: true})
		if err != nil || got.RetentionDays != days {
			t.Errorf("retention %d: %+v, %v", days, got, err)
		}
	}
	// A value out of range in the table reads as the default.
	testdb.Must(t, ss.SetInt(ctx, models.SettingNetToolsRetentionDays, 9999))
	testdb.Must(t, ss.SetString(ctx, models.SettingNetToolsAllowlist, `not json`))
	if got := LoadSettings(ctx, ss); got.RetentionDays != 30 || len(got.Allowlist) != 0 {
		t.Errorf("bad stored values read as %+v, want 30 days and an empty allowlist", got)
	}
}
