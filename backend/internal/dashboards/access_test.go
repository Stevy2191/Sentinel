package dashboards

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

func TestResolveAccess(t *testing.T) {
	owner, someone := uuid.New(), uuid.New()
	site := uuid.New()
	personal := &models.Dashboard{OwnerID: &owner}
	siteDash := &models.Dashboard{SiteID: &site}
	cases := []struct {
		name  string
		v     Viewer
		d     *models.Dashboard
		site  services.SiteAccessLevel
		share string
		want  Level
	}{
		{"admin, personal", Viewer{UserID: someone, IsAdmin: true}, personal, 0, "", LevelManage},
		{"admin, site", Viewer{UserID: someone, IsAdmin: true}, siteDash, 0, "", LevelManage},
		{"public never", PublicViewer, siteDash, services.SiteAccessEditable, "", LevelNone},
		{"owner", Viewer{UserID: owner}, personal, 0, "", LevelEdit},
		{"shared readonly", Viewer{UserID: someone}, personal, 0, models.PermissionReadonly, LevelView},
		{"shared editable", Viewer{UserID: someone}, personal, 0, models.PermissionEditable, LevelEdit},
		{"stranger, personal", Viewer{UserID: someone}, personal, 0, "", LevelNone},
		{"unknown share", Viewer{UserID: someone}, personal, 0, "owner", LevelNone},
		{"site readonly", Viewer{UserID: someone}, siteDash, services.SiteAccessReadonly, "", LevelView},
		{"site editable", Viewer{UserID: someone}, siteDash, services.SiteAccessEditable, "", LevelEdit},
		{"site none", Viewer{UserID: someone}, siteDash, services.SiteAccessNone, "", LevelNone},
		{"site share ignored on personal", Viewer{UserID: someone}, personal, services.SiteAccessEditable, "", LevelNone},
		{"personal share ignored on site", Viewer{UserID: someone}, siteDash, services.SiteAccessNone, models.PermissionEditable, LevelNone},
	}
	for _, c := range cases {
		if got := resolveAccess(c.v, c.d, c.site, c.share); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCanChangeContent(t *testing.T) {
	cases := []struct {
		l         Level
		published bool
		want      bool
	}{
		{LevelView, false, false}, {LevelEdit, false, true}, {LevelManage, false, true},
		{LevelEdit, true, false}, {LevelManage, true, true},
	}
	for _, c := range cases {
		if got := canChangeContent(c.l, c.published); got != c.want {
			t.Errorf("canChangeContent(%v, published=%v) = %v, want %v", c.l, c.published, got, c.want)
		}
	}
}

func TestCanShare(t *testing.T) {
	owner, someone, site := uuid.New(), uuid.New(), uuid.New()
	personal := &models.Dashboard{OwnerID: &owner}
	siteDash := &models.Dashboard{SiteID: &site}
	if !canShare(Viewer{UserID: owner}, personal) {
		t.Error("the owner cannot share their dashboard")
	}
	if canShare(Viewer{UserID: someone}, personal) {
		t.Error("a stranger can share someone's dashboard")
	}
	if !canShare(Viewer{UserID: someone, IsAdmin: true}, personal) {
		t.Error("an admin cannot share a personal dashboard")
	}
	if canShare(PublicViewer, personal) {
		t.Error("a public viewer can share")
	}
	if canShare(Viewer{UserID: someone}, siteDash) {
		t.Error("a member can share a site dashboard (it follows the site's sharing)")
	}
}

func TestLevelString(t *testing.T) {
	for l, want := range map[Level]string{LevelNone: "none", LevelView: "view", LevelEdit: "edit", LevelManage: "manage"} {
		if l.String() != want {
			t.Errorf("Level(%d).String() = %q, want %q", l, l.String(), want)
		}
	}
}
