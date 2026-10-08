package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// siteLabels is what the monitor and agent handlers need from sites: whether
// the caller may label something with a site, and the names to show.
type siteLabels interface {
	SiteAccess(ctx context.Context, userID uuid.UUID, isAdmin bool, siteID uuid.UUID) (services.SiteAccessLevel, error)
	NamesByID(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
}

// siteField is an optional site_id read from a request body. Set is false
// when the field was left out (keep the current site); Set with a nil ID is
// an explicit null (clear it).
type siteField struct {
	Set bool
	ID  *uuid.UUID
}

// parseSiteField reads site_id as captured by a json.RawMessage, which is nil
// when the field was left out and the literal null when it was sent as null.
func parseSiteField(raw json.RawMessage) (siteField, error) {
	if raw == nil {
		return siteField{}, nil
	}
	if string(bytes.TrimSpace(raw)) == "null" {
		return siteField{Set: true}, nil
	}
	var id uuid.UUID
	if err := json.Unmarshal(raw, &id); err != nil {
		return siteField{}, errors.New("site_id must be a site id or null")
	}
	return siteField{Set: true, ID: &id}, nil
}

// requireAssignableSite answers 400 "site not found" unless the caller may
// label something with siteID: an admin any site that exists, anyone else a
// site shared with them. A missing site and someone else's get the same
// answer, so the response does not reveal which sites exist.
func requireAssignableSite(c *gin.Context, sites siteLabels, siteID uuid.UUID) bool {
	userID, _, isAdmin, _ := GetUserFromContext(c)
	level, err := sites.SiteAccess(c.Request.Context(), userID, isAdmin, siteID)
	if errors.Is(err, services.ErrSiteNotFound) || (err == nil && level == services.SiteAccessNone) {
		respondError(c, http.StatusBadRequest, "site not found")
		return false
	}
	if err != nil {
		respondInternal(c, "site access", err)
		return false
	}
	return true
}

// siteNameOf is the name to show for siteID, or nil.
func siteNameOf(names map[uuid.UUID]string, siteID *uuid.UUID) *string {
	if siteID == nil {
		return nil
	}
	if name, ok := names[*siteID]; ok {
		return &name
	}
	return nil
}

// siteNames looks up the names for ids. A failure is logged and gives no
// names: a site name is a label, and the list it labels is still worth
// showing without it.
func siteNames(ctx context.Context, sites siteLabels, ids []uuid.UUID) map[uuid.UUID]string {
	if len(ids) == 0 {
		return nil
	}
	names, err := sites.NamesByID(ctx, ids)
	if err != nil {
		log.Printf("[sites] labelling with site names: %v", err)
		return nil
	}
	return names
}

// labelMonitorSites fills SiteName on each monitor.
func labelMonitorSites(ctx context.Context, sites siteLabels, monitors []*models.Monitor) {
	ids := make([]uuid.UUID, 0, len(monitors))
	for _, m := range monitors {
		if m.SiteID != nil {
			ids = append(ids, *m.SiteID)
		}
	}
	names := siteNames(ctx, sites, ids)
	for _, m := range monitors {
		m.SiteName = siteNameOf(names, m.SiteID)
	}
}

// labelAgentSites fills SiteName on each agent.
func labelAgentSites(ctx context.Context, sites siteLabels, agents []*models.Agent) {
	ids := make([]uuid.UUID, 0, len(agents))
	for _, a := range agents {
		if a.SiteID != nil {
			ids = append(ids, *a.SiteID)
		}
	}
	names := siteNames(ctx, sites, ids)
	for _, a := range agents {
		a.SiteName = siteNameOf(names, a.SiteID)
	}
}
