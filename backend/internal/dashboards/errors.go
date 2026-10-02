package dashboards

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound covers "missing" and "not yours": the API answers 404 for both.
	ErrNotFound = errors.New("dashboard not found")
	// ErrForbidden: the caller can see the dashboard but not do this to it.
	ErrForbidden = errors.New("you need edit access to do that")
	// ErrPublished: an editor tried to change a published dashboard.
	ErrPublished = errors.New("this dashboard is published; ask an admin to change it")
	// ErrInvalid wraps a validation message for the dashboard itself.
	ErrInvalid = errors.New("invalid dashboard")
	// ErrSiteNotFound: a site dashboard for a site the caller cannot see.
	ErrSiteNotFound = errors.New("site not found")
	// ErrWidgetNotFound: no such widget on this dashboard.
	ErrWidgetNotFound = errors.New("widget not found")
	// ErrShareSiteDashboard: site dashboards follow their site's sharing.
	ErrShareSiteDashboard = errors.New("a site dashboard is shared through its site")
	// ErrShareOwner: sharing a dashboard with its own owner.
	ErrShareOwner = errors.New("the owner already has this dashboard")
	// ErrUnknownUser: sharing with a user who does not exist.
	ErrUnknownUser = errors.New("no such user")
	// ErrNoLink: revoking a link that does not exist.
	ErrNoLink = errors.New("this dashboard has no public link")
)

// msgSubjectUnavailable is the one message for a widget subject that is hidden
// from the editor and one that does not exist, so neither Save nor Preview can
// be used to probe which ids exist.
const msgSubjectUnavailable = "uses a site, device, monitor or server that does not exist or that you cannot see"

// invalid wraps msg as an ErrInvalid.
func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalid, msg) }

// VersionConflictError: a save carried an older version than the stored one.
type VersionConflictError struct{ Current int }

func (e *VersionConflictError) Error() string {
	return fmt.Sprintf("someone else saved this dashboard (it is now at version %d)", e.Current)
}

// WidgetError is a save rejected because of one widget; Index is its position
// in the request (0-based), Field the config field or "type"/"title"/"position".
type WidgetError struct {
	Index int
	Field string
	Msg   string
}

func (e *WidgetError) Error() string {
	return fmt.Sprintf("widget %d: %s %s", e.Index+1, e.Field, e.Msg)
}
