package dashboards

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Widget data states (spec §4). Every state is a 200 response.
const (
	StateOK       = "ok"
	StateNoAccess = "no_access"
	StateRemoved  = "removed"
	StateNoData   = "no_data"
)

// Subjects is what a widget's config refers to, for access checks. A port is
// listed as its device: a port's access is its device's.
type Subjects struct {
	Sites    []uuid.UUID
	Devices  []uuid.UUID
	Monitors []uuid.UUID
	Agents   []uuid.UUID
	// Broad: the widget also shows things chosen by the viewer's own access
	// ("all open incidents I can see"), so its rows differ between viewers.
	Broad bool
}

func (s Subjects) count() int {
	return len(s.Sites) + len(s.Devices) + len(s.Monitors) + len(s.Agents)
}

// Filtered is a widget's subjects sorted by what one viewer may see.
type Filtered struct {
	Visible Subjects
	Hidden  int
	Removed int
	total   int
}

// State is the response state when none of the widget's subjects remain
// visible, or "" when the widget should resolve. A widget with no subjects
// (a label) or a broad scope always resolves.
func (f Filtered) State() string {
	if f.total == 0 || f.Visible.count() > 0 {
		return ""
	}
	if f.Hidden > 0 {
		return StateNoAccess
	}
	return StateRemoved
}

// SiteLeveler is services.SiteService.SiteAccess.
type SiteLeveler interface {
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
}

// MonitorViewer is services.MonitorService.CanUserViewMonitor.
type MonitorViewer interface {
	CanUserViewMonitor(ctx context.Context, userID, monitorID uuid.UUID) (bool, error)
}

// Checker answers existence and visibility questions by the existing rules.
// Taken as an interface so the resolver is tested without a database.
type Checker interface {
	// DeviceSites maps each existing device to its site; missing ones are absent.
	DeviceSites(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error)
	// SiteLevel is the viewer's access to a site; services.ErrSiteNotFound
	// when it does not exist.
	SiteLevel(ctx context.Context, v Viewer, siteID uuid.UUID) (services.SiteAccessLevel, error)
	ExistingMonitors(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	MonitorVisible(ctx context.Context, v Viewer, id uuid.UUID) (bool, error)
	ExistingAgents(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
}

// DBChecker is the Checker over the real tables and services.
type DBChecker struct {
	db       *gorm.DB
	sites    SiteLeveler
	monitors MonitorViewer
}

// NewDBChecker builds the production Checker.
func NewDBChecker(db *gorm.DB, sites SiteLeveler, monitors MonitorViewer) *DBChecker {
	return &DBChecker{db: db, sites: sites, monitors: monitors}
}

// DeviceSites implements Checker.
func (c *DBChecker) DeviceSites(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	var rows []struct {
		ID     uuid.UUID
		SiteID uuid.UUID
	}
	if err := c.db.WithContext(ctx).Table("devices").Select("id, site_id").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("loading device sites: %w", err)
	}
	out := make(map[uuid.UUID]uuid.UUID, len(rows))
	for _, r := range rows {
		out[r.ID] = r.SiteID
	}
	return out, nil
}

// SiteLevel implements Checker. A public viewer is answered as an admin: an
// admin published the dashboard.
func (c *DBChecker) SiteLevel(ctx context.Context, v Viewer, siteID uuid.UUID) (services.SiteAccessLevel, error) {
	return c.sites.SiteAccess(ctx, v.UserID, v.IsAdmin || v.Public, siteID)
}

// ExistingMonitors implements Checker.
func (c *DBChecker) ExistingMonitors(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return c.existing(ctx, "monitors", ids)
}

// MonitorVisible implements Checker, by the monitor list's own rule.
func (c *DBChecker) MonitorVisible(ctx context.Context, v Viewer, id uuid.UUID) (bool, error) {
	return c.monitors.CanUserViewMonitor(ctx, v.UserID, id)
}

// ExistingAgents implements Checker.
func (c *DBChecker) ExistingAgents(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return c.existing(ctx, "agents", ids)
}

// existing reports which ids exist in table (a constant, never input).
func (c *DBChecker) existing(ctx context.Context, table string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	var found []uuid.UUID
	if err := c.db.WithContext(ctx).Table(table).Where("id IN ?", ids).Pluck("id", &found).Error; err != nil {
		return nil, fmt.Errorf("checking %s: %w", table, err)
	}
	out := make(map[uuid.UUID]bool, len(found))
	for _, id := range found {
		out[id] = true
	}
	return out, nil
}

const (
	subjectVisible = iota + 1
	subjectHidden
	subjectRemoved
)

// Filter sorts s into what v may see, what v may not, and what no longer
// exists. Admins and public viewers see everything that exists. This is the
// only place widget subjects are access-checked; widgets resolve only what
// Filter hands them.
func Filter(ctx context.Context, c Checker, v Viewer, s Subjects) (Filtered, error) {
	out := Filtered{total: s.count()}
	out.Visible.Broad = s.Broad
	everything := v.IsAdmin || v.Public

	siteSeen := map[uuid.UUID]int{}
	siteState := func(id uuid.UUID) (int, error) {
		if st, ok := siteSeen[id]; ok {
			return st, nil
		}
		level, err := c.SiteLevel(ctx, v, id)
		st := subjectVisible
		switch {
		case errors.Is(err, services.ErrSiteNotFound):
			st = subjectRemoved
		case err != nil:
			return 0, err
		case level < services.SiteAccessReadonly:
			st = subjectHidden
		}
		siteSeen[id] = st
		return st, nil
	}
	tally := func(st int) bool {
		switch st {
		case subjectHidden:
			out.Hidden++
		case subjectRemoved:
			out.Removed++
		}
		return st == subjectVisible
	}

	for _, id := range s.Sites {
		st, err := siteState(id)
		if err != nil {
			return out, err
		}
		if tally(st) {
			out.Visible.Sites = append(out.Visible.Sites, id)
		}
	}
	if len(s.Devices) > 0 {
		sites, err := c.DeviceSites(ctx, s.Devices)
		if err != nil {
			return out, err
		}
		for _, id := range s.Devices {
			siteID, ok := sites[id]
			if !ok {
				out.Removed++
				continue
			}
			st, err := siteState(siteID)
			if err != nil {
				return out, err
			}
			if tally(st) {
				out.Visible.Devices = append(out.Visible.Devices, id)
			}
		}
	}
	if len(s.Monitors) > 0 {
		exist, err := c.ExistingMonitors(ctx, s.Monitors)
		if err != nil {
			return out, err
		}
		for _, id := range s.Monitors {
			if !exist[id] {
				out.Removed++
				continue
			}
			ok := everything
			if !ok {
				if ok, err = c.MonitorVisible(ctx, v, id); err != nil {
					return out, err
				}
			}
			if ok {
				out.Visible.Monitors = append(out.Visible.Monitors, id)
			} else {
				out.Hidden++
			}
		}
	}
	if len(s.Agents) > 0 {
		exist, err := c.ExistingAgents(ctx, s.Agents)
		if err != nil {
			return out, err
		}
		for _, id := range s.Agents {
			switch {
			case !exist[id]:
				out.Removed++
			case everything:
				out.Visible.Agents = append(out.Visible.Agents, id)
			default:
				out.Hidden++
			}
		}
	}
	return out, nil
}
