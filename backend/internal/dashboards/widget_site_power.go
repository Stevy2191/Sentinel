package dashboards

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// siteConfig is the config of widgets about one site.
type siteConfig struct {
	SiteID uuid.UUID `json:"site_id"`
}

func validateSiteConfig(raw json.RawMessage) (json.RawMessage, error) {
	var c siteConfig
	if err := decodeConfig(raw, &c); err != nil {
		return nil, err
	}
	if c.SiteID == uuid.Nil {
		return nil, fieldErr("site_id", "is required")
	}
	return json.Marshal(c)
}

func siteSubjects(raw json.RawMessage) Subjects {
	var c siteConfig
	_ = json.Unmarshal(raw, &c)
	if c.SiteID == uuid.Nil {
		return Subjects{}
	}
	return Subjects{Sites: []uuid.UUID{c.SiteID}}
}

// UPSCard is one UPS: its reachability, latest readings and open conditions.
type UPSCard struct {
	DeviceID      *uuid.UUID         `json:"device_id,omitempty"`
	Name          string             `json:"name"`
	Status        string             `json:"status"`
	Readings      map[string]float64 `json:"readings"`
	Conditions    []string           `json:"conditions"`
	LowBatteryPct int                `json:"low_battery_pct"`
	HighLoadPct   int                `json:"high_load_pct"`
}

// SitePowerData is every UPS in a site.
type SitePowerData struct {
	UPSes []UPSCard `json:"upses"`
}

type sitePowerWidget struct {
	devices DeviceReader
	ports   PortReader
}

func (sitePowerWidget) Type() string { return "site_power" }

func (sitePowerWidget) Validate(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
	return validateSiteConfig(raw)
}

func (sitePowerWidget) Subjects(raw json.RawMessage) Subjects { return siteSubjects(raw) }

func (sitePowerWidget) Refresh(json.RawMessage, string) time.Duration { return statusRefresh }

// Resolve lists the site's UPSes (chosen or detected type ups) and reads each
// one's status with the same code as the device page's Power panel.
func (w sitePowerWidget) Resolve(ctx context.Context, _ json.RawMessage, in ResolveInput) (any, error) {
	if len(in.Visible.Sites) == 0 {
		return nil, ErrNoData
	}
	site := in.Visible.Sites[0]
	list, err := w.devices.List(ctx, uuid.Nil, true, services.DeviceFilter{SiteID: &site})
	if err != nil {
		return nil, err
	}
	out := SitePowerData{UPSes: []UPSCard{}}
	for i := range list {
		d := &list[i]
		if d.EffectiveType != models.DeviceTypeUPS {
			continue
		}
		st, err := w.ports.UPSStatus(ctx, d)
		if err != nil {
			return nil, err
		}
		conds := st.Conditions
		if conds == nil {
			conds = []string{}
		}
		out.UPSes = append(out.UPSes, UPSCard{DeviceID: idFor(in.Viewer, d.ID), Name: d.Name, Status: d.Status,
			Readings: st.Readings, Conditions: conds, LowBatteryPct: st.LowBatteryPct, HighLoadPct: st.HighLoadPct})
	}
	if len(out.UPSes) == 0 {
		return nil, ErrNoData
	}
	return out, nil
}
