package netreport

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// MetricChoice is one metric the Metrics step offers.
type MetricChoice struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unit   string `json:"unit"`
	Source string `json:"source"` // "builtin" | "profile" | "custom"
}

// Preview is what the editor shows under a scope: its size now, and the
// metrics a report on it can show, with the defaults pre-filled.
type Preview struct {
	Ports    int            `json:"ports"`
	Devices  int            `json:"devices"`
	Capped   bool           `json:"capped"`
	Metrics  []MetricChoice `json:"metrics"`
	Defaults []string       `json:"defaults"`
}

// The editor's pre-filled metrics (spec §1).
var (
	portDefaults    = []string{services.MetricIfInBps, services.MetricIfOutBps, services.MetricIfInUtilPct, services.MetricIfOutUtilPct}
	trafficDefaults = []string{services.MetricIfInBps, services.MetricIfOutBps}
	upsReadings     = []string{services.MetricUPSChargePct, services.MetricUPSLoadPct, services.MetricUPSRuntimeMin, services.MetricUPSOnBattery}
)

// Preview sizes a scope as requester and lists its metrics; scope.Metrics is ignored.
// Ports and Devices are both filled for every scope type; Capped is set when
// the subjects (devices for a devices scope, ports otherwise) exceed
// models.MaxReportSubjects, so the report will keep the busiest of them.
func (b *Builder) Preview(ctx context.Context, requester uuid.UUID, scopeType string, scope models.ReportScope) (*Preview, error) {
	if !models.IsNetworkScope(scopeType) {
		return nil, &FieldError{Field: "scope_type", Message: msgNetworkScopeTypes}
	}
	if err := scope.ValidateSubjects(scopeType); err != nil {
		return nil, &FieldError{Field: "scope_data", Message: err.Error()}
	}
	res, err := b.resolve(ctx, requester, scopeType, scope)
	if err != nil {
		return nil, err
	}
	if res.unavailable > 0 {
		return nil, notAvailable()
	}
	choices, err := b.choices(ctx, res)
	if err != nil {
		return nil, err
	}
	p := &Preview{Ports: len(res.ports), Devices: len(res.deviceIDs()), Metrics: choices, Defaults: defaults(scopeType, choices)}
	subjects := p.Ports
	if scopeType == models.ScopeTypeDevices {
		p.Ports = res.physicalPorts()
		subjects = p.Devices
	}
	p.Capped = subjects > models.MaxReportSubjects
	return p, nil
}

// choices lists the metrics a report on this scope can show: those with
// stored series in it, plus its fixed defaults so the editor can always name
// them. Text metrics, link speed and (on ports and port roles) device
// metrics are left out.
func (b *Builder) choices(ctx context.Context, res *resolved) ([]MetricChoice, error) {
	keys, err := b.storedMetrics(ctx, res)
	if err != nil {
		return nil, err
	}
	keys = append(keys, fixedDefaults(res.scopeType)...)
	infos, err := b.lookupMetrics(ctx, keys)
	if err != nil {
		return nil, err
	}
	list := make([]metricInfo, 0, len(infos))
	for _, m := range infos {
		if m.allowedOn(res.scopeType) {
			list = append(list, m)
		}
	}
	sort.Slice(list, func(i, j int) bool { return lessMetric(list[i], list[j]) })
	out := make([]MetricChoice, len(list))
	for i, m := range list {
		out[i] = MetricChoice{Key: m.Key, Label: m.Label, Unit: m.Unit, Source: m.Source}
	}
	return out, nil
}

// storedMetrics lists the metrics with series in the scope: on its ports for
// a ports or port_roles scope, on its devices otherwise.
func (b *Builder) storedMetrics(ctx context.Context, res *resolved) ([]string, error) {
	var keys []string
	var err error
	q := b.db.WithContext(ctx)
	if deviceScope(res.scopeType) {
		if ids := res.deviceIDs(); len(ids) > 0 {
			err = q.Raw(`SELECT DISTINCT metric FROM metrics.series WHERE device_id IN ?`, ids).Scan(&keys).Error
		}
	} else if ids := res.portIDs(); len(ids) > 0 {
		err = q.Raw(`SELECT DISTINCT metric FROM metrics.series WHERE interface_id IN ?`, ids).Scan(&keys).Error
	}
	if err != nil {
		return nil, fmt.Errorf("listing the scope's metrics: %w", err)
	}
	return keys, nil
}

// lessMetric orders built-in metrics first, in catalogue order, then profile
// metrics by profile name and position.
func lessMetric(a, b metricInfo) bool {
	if (a.Source == sourceBuiltin) != (b.Source == sourceBuiltin) {
		return a.Source == sourceBuiltin
	}
	if a.profile != b.profile {
		return a.profile < b.profile
	}
	if a.order != b.order {
		return a.order < b.order
	}
	return a.Key < b.Key
}

// fixedDefaults are the defaults that do not depend on what the devices
// have: the port metrics for ports and port roles, traffic for sites (and
// the devices scope's fallback).
func fixedDefaults(scopeType string) []string {
	if scopeType == models.ScopeTypePorts || scopeType == models.ScopeTypePortRoles {
		return portDefaults
	}
	return trafficDefaults
}

// defaults is the editor's pre-filled list (spec §1). Devices get their
// profile metrics, then the UPS readings any of them has, at most 10; or
// traffic in and out (as device totals) when neither applies.
func defaults(scopeType string, choices []MetricChoice) []string {
	if scopeType != models.ScopeTypeDevices {
		return slices.Clone(fixedDefaults(scopeType))
	}
	var out []string
	for _, c := range choices {
		if c.Source != sourceBuiltin {
			out = append(out, c.Key)
		}
	}
	for _, k := range upsReadings {
		if slices.ContainsFunc(choices, func(c MetricChoice) bool { return c.Key == k }) {
			out = append(out, k)
		}
	}
	if len(out) > models.MaxReportMetrics {
		out = out[:models.MaxReportMetrics]
	}
	if len(out) == 0 {
		return slices.Clone(trafficDefaults)
	}
	return out
}
