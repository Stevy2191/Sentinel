package services

import "github.com/Stevy2191/Sentinel/backend/internal/models"

// SiteAccessLevel is what a user may do in a site. Levels are ordered: each
// includes everything below it, so callers compare with >=.
type SiteAccessLevel int

const (
	// SiteAccessNone: the site does not exist as far as this user is concerned.
	SiteAccessNone SiteAccessLevel = iota
	// SiteAccessReadonly: see the site and everything in it.
	SiteAccessReadonly
	// SiteAccessEditable: also change what is inside the site (devices, maps,
	// dashboards, in later phases), but not the site itself.
	SiteAccessEditable
	// SiteAccessAdmin: also rename or delete the site and manage its sharing.
	SiteAccessAdmin
)

// String is the level's name as the API reports it.
func (l SiteAccessLevel) String() string {
	switch l {
	case SiteAccessReadonly:
		return "readonly"
	case SiteAccessEditable:
		return "editable"
	case SiteAccessAdmin:
		return "admin"
	default:
		return "none"
	}
}

// resolveSiteAccess holds every site permission rule, with no database, so
// the rules are tested directly. Admins manage every site; everyone else gets
// exactly what their share says, and anything unrecognised is no access.
func resolveSiteAccess(isAdmin bool, share *models.SiteSharing) SiteAccessLevel {
	if isAdmin {
		return SiteAccessAdmin
	}
	if share == nil {
		return SiteAccessNone
	}
	switch share.Permission {
	case models.PermissionReadonly:
		return SiteAccessReadonly
	case models.PermissionEditable:
		return SiteAccessEditable
	default:
		return SiteAccessNone
	}
}
