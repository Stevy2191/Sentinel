package dashboards

import (
	"context"

	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// additiveUnits are the units whose series add up to a meaningful total:
// rates of things, bits per second ("bps": port traffic and link speed) and
// counts per minute ("per_min": port errors and discards). Every other unit
// the catalogue and the metric profiles use (%, °C, V, min, bool, enum, and
// whatever a custom metric names) is averaged instead: four ports at 50 %
// busy are a device 50 % busy, not 200 %.
var additiveUnits = map[string]bool{"bps": true, "per_min": true}

// setTotals makes q combine its series into totals, one per metric (or, with
// perDevice, one per device and metric): series of an additive unit are
// added up and the rest averaged, per time step, so a stat's value, its
// sparkline and a chart's line all come from the same numbers. When every
// metric is a port metric, only the devices' physical interfaces count, as
// on the site traffic chart; ErrNoData when they have none. q's DeviceIDs
// and Metrics must be set; defs describes the metrics.
func setTotals(ctx context.Context, ports PortReader, q *services.MetricsQuery, defs map[string]services.MetricDef, perDevice bool) error {
	q.Sum, q.PerDevice, q.Average = true, perDevice, nil
	for _, k := range q.Metrics {
		if !additiveUnits[defs[k].Unit] {
			q.Average = append(q.Average, k)
		}
	}
	if !allPortMetrics(q.Metrics) {
		return nil
	}
	ifs, err := ports.PhysicalInterfaceIDs(ctx, q.DeviceIDs)
	if err != nil {
		return err
	}
	if len(ifs) == 0 {
		return ErrNoData
	}
	q.InterfaceIDs = ifs
	return nil
}
