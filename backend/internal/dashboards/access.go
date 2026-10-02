package dashboards

import (
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Level is what a viewer may do with a dashboard. Levels are ordered: each
// includes everything below it, so callers compare with >=.
type Level int

const (
	// LevelNone: the dashboard does not exist as far as this viewer knows.
	LevelNone Level = iota
	// LevelView: see the dashboard and load its widgets.
	LevelView
	// LevelEdit: also change it (unless it is published) and see its configs.
	LevelEdit
	// LevelManage: an admin: everything, published or not.
	LevelManage
)

// String is the level's name as the API reports it.
func (l Level) String() string {
	switch l {
	case LevelView:
		return "view"
	case LevelEdit:
		return "edit"
	case LevelManage:
		return "manage"
	default:
		return "none"
	}
}

// Viewer is who is asking. A Public viewer is a public-link request: it has
// no user, and the logged-in routes never grant it anything.
type Viewer struct {
	UserID  uuid.UUID
	IsAdmin bool
	Public  bool
}

// PublicViewer is the viewer of every public-link request.
var PublicViewer = Viewer{Public: true}

// resolveAccess holds every dashboard permission rule, with no database, so
// the rules are tested directly. siteLevel is the viewer's access to a site
// dashboard's site and is ignored for a personal one; share is the viewer's
// share on a personal dashboard ("" for none) and is ignored for a site's.
func resolveAccess(v Viewer, d *models.Dashboard, siteLevel services.SiteAccessLevel, share string) Level {
	if v.Public {
		return LevelNone
	}
	if v.IsAdmin {
		return LevelManage
	}
	if d.SiteID != nil {
		switch {
		case siteLevel >= services.SiteAccessEditable:
			return LevelEdit
		case siteLevel >= services.SiteAccessReadonly:
			return LevelView
		}
		return LevelNone
	}
	if d.OwnerID != nil && *d.OwnerID == v.UserID {
		return LevelEdit
	}
	switch share {
	case models.PermissionEditable:
		return LevelEdit
	case models.PermissionReadonly:
		return LevelView
	}
	return LevelNone
}

// sitePermissionLevel turns a site_sharing permission into the level
// resolveSiteAccess would give it ("" or anything unknown is none).
func sitePermissionLevel(p string) services.SiteAccessLevel {
	switch p {
	case models.PermissionReadonly:
		return services.SiteAccessReadonly
	case models.PermissionEditable:
		return services.SiteAccessEditable
	}
	return services.SiteAccessNone
}

// canChangeContent reports whether l may change a dashboard's name,
// description, widgets or layout, or delete it. A published dashboard needs
// manage, so what is public stays what an admin approved.
func canChangeContent(l Level, published bool) bool {
	if published {
		return l >= LevelManage
	}
	return l >= LevelEdit
}

// canShare reports whether v may change who a dashboard is shared with: the
// owner of a personal dashboard, or an admin. A site dashboard follows its
// site's sharing, which only admins change.
func canShare(v Viewer, d *models.Dashboard) bool {
	if v.Public {
		return false
	}
	if v.IsAdmin {
		return true
	}
	return d.SiteID == nil && d.OwnerID != nil && *d.OwnerID == v.UserID
}
