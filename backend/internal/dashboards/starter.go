package dashboards

import (
	"encoding/json"

	"github.com/google/uuid"
)

// starterWidgets are the standard widgets a new site dashboard can start
// with (spec §3): internet traffic and power on top, open incidents and the
// busiest ports below, then the site's devices.
func starterWidgets(siteID uuid.UUID) []WidgetInput {
	cfg := func(v any) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}
	return []WidgetInput{
		{Type: "timeseries", Title: "Internet traffic", X: 0, Y: 0, W: 8, H: 4,
			Config: cfg(map[string]any{"source": sourceSiteTraffic, "site_id": siteID, "view": "internet", "range": "24h"})},
		{Type: "site_power", Title: "Power", X: 8, Y: 0, W: 4, H: 4,
			Config: cfg(map[string]any{"site_id": siteID})},
		{Type: "open_incidents", Title: "Open incidents", X: 0, Y: 4, W: 6, H: 4,
			Config: cfg(map[string]any{"scope": "site", "site_id": siteID, "limit": 20})},
		{Type: "top_n", Title: "Busiest ports", X: 6, Y: 4, W: 6, H: 4,
			Config: cfg(map[string]any{"site_id": siteID, "measure": "traffic", "n": 10, "range": "24h"})},
		{Type: "device_table", Title: "Devices", X: 0, Y: 8, W: 12, H: 5,
			Config: cfg(map[string]any{"site_id": siteID})},
	}
}
