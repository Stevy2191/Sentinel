package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// SiteMapReader is what the site map widget reads (SiteMapService and
// LinkPoller together).
type SiteMapReader interface {
	Snapshot(ctx context.Context, v services.Viewer, siteID uuid.UUID) (*services.MapSnapshot, error)
	PublicSnapshot(ctx context.Context, siteID uuid.UUID) (*services.MapSnapshot, error)
	Latest(ctx context.Context, siteID uuid.UUID) services.MapTraffic
	Touch(siteID uuid.UUID)
}

// SiteMapData is a site's map and its latest traffic.
type SiteMapData struct {
	Map     *services.MapSnapshot `json:"map"`
	Traffic services.MapTraffic   `json:"traffic"`
}

// siteMapRefresh matches the fast poll.
const siteMapRefresh = 10 * time.Second

type siteMapWidget struct{ maps SiteMapReader }

func (siteMapWidget) Type() string { return "site_map" }

func (siteMapWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateSiteConfig(raw)
}

func (siteMapWidget) Subjects(raw json.RawMessage) Subjects { return siteSubjects(raw) }

func (siteMapWidget) Refresh(json.RawMessage, string) time.Duration { return siteMapRefresh }

// Resolve is the site's map, trimmed for a public viewer. Asking counts as
// watching the map, so its links are fast-polled for the next minute.
func (w siteMapWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Sites) == 0 || w.maps == nil {
		return nil, ErrNoData
	}
	site := in.Visible.Sites[0]
	var m *services.MapSnapshot
	var err error
	if in.Viewer.Public {
		m, err = w.maps.PublicSnapshot(ctx, site)
	} else {
		m, err = w.maps.Snapshot(ctx, services.Viewer{UserID: in.Viewer.UserID, IsAdmin: in.Viewer.IsAdmin}, site)
	}
	if err != nil {
		return nil, err
	}
	if len(m.Devices) == 0 {
		return nil, ErrNoData
	}
	// A widget never edits: nothing hidden or addable is offered.
	m.CanEdit, m.IsAdmin = false, false
	m.HiddenDevices, m.HiddenLinks, m.Candidates = []services.MapDevice{}, []services.MapLink{}, []services.MapDevice{}
	w.maps.Touch(site)
	t := w.maps.Latest(ctx, site)
	if in.Viewer.Public {
		t = services.PublicTraffic(site, t)
	}
	return SiteMapData{Map: m, Traffic: t}, nil
}
