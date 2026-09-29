package services

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

func TestResolveSiteAccess(t *testing.T) {
	share := func(p string) *models.SiteSharing { return &models.SiteSharing{Permission: p} }

	cases := []struct {
		name    string
		isAdmin bool
		share   *models.SiteSharing
		want    SiteAccessLevel
	}{
		{"admin without a share", true, nil, SiteAccessAdmin},
		// A share never lowers an admin: sharing a site with an admin must not
		// take away their ability to manage it.
		{"admin with a readonly share", true, share(models.PermissionReadonly), SiteAccessAdmin},
		{"readonly share", false, share(models.PermissionReadonly), SiteAccessReadonly},
		{"editable share", false, share(models.PermissionEditable), SiteAccessEditable},
		{"no share", false, nil, SiteAccessNone},
		// Anything unrecognised fails closed. A hand-edited row or a value
		// added by a future version must not be read as more access.
		{"unknown permission", false, share("owner"), SiteAccessNone},
		{"empty permission", false, share(""), SiteAccessNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveSiteAccess(tc.isAdmin, tc.share); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSiteAccessLevelString(t *testing.T) {
	for level, want := range map[SiteAccessLevel]string{
		SiteAccessNone: "none", SiteAccessReadonly: "readonly",
		SiteAccessEditable: "editable", SiteAccessAdmin: "admin",
	} {
		if got := level.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", level, got, want)
		}
	}
}
