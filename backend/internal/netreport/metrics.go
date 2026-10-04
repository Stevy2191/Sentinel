package netreport

import (
	"context"
	"fmt"
	"strings"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
)

// Where a metric comes from, as the Metrics step labels it.
const (
	sourceBuiltin = "builtin" // Sentinel's own catalogue (ports, UPS)
	sourceProfile = "profile" // a metric of a built-in profile (Cisco switch health)
	sourceCustom  = "custom"  // a metric of a profile an admin made
)

// metricInfo is a metric as a report needs to know it.
type metricInfo struct {
	Key, Label, Unit string
	Source           string
	// Kind is a profile metric's kind (gauge, counter, status); "" built-in.
	Kind string
	// order and profile sort the choices: the catalogue's order for built-in
	// metrics; profile name (lower case), then position, for the others.
	order   int
	profile string
}

// port reports a built-in port metric, which has a series per port. Custom
// keys cannot start with if_ (profile_metrics' CHECK).
func (m metricInfo) port() bool { return strings.HasPrefix(m.Key, "if_") }

// additive reports a unit whose series add up when lines combine (bps,
// per_min); every other unit is averaged (phase 4's rule).
func (m metricInfo) additive() bool { return m.Unit == "bps" || m.Unit == "per_min" }

// text reports a metric whose values are codes, not amounts: an enum, or a
// profile's status metric (a fan's state). Its average means nothing.
func (m metricInfo) text() bool { return m.Unit == "enum" || m.Kind == models.MetricKindStatus }

// reportable reports whether a report can show m: not a text metric, and not
// link speed (bits per second that are not traffic, whose "bytes moved"
// would be nonsense).
func (m metricInfo) reportable() bool { return !m.text() && m.Key != services.MetricIfSpeedBps }

// allowedOn reports whether a report on scopeType can show m: m is
// reportable, and the scope has lines for it (a port metric on any scope, a
// device metric only on a devices or sites scope). It is the one rule that
// create (checkMetrics), the preview's choices and the run all apply.
func (m metricInfo) allowedOn(scopeType string) bool {
	return m.reportable() && (m.port() || deviceScope(scopeType))
}

// builtinInfo describes a key of Sentinel's own catalogue.
func builtinInfo(key string) (metricInfo, bool) {
	for i, d := range services.MetricCatalogue {
		if d.Key == key {
			return metricInfo{Key: d.Key, Label: d.Label, Unit: d.Unit, Source: sourceBuiltin, order: i}, true
		}
	}
	return metricInfo{}, false
}

// lookupMetrics describes these keys: the built-in catalogue first, then
// profile_metrics, read directly (not through the in-memory key registry, so
// a report never depends on when that was last loaded). Unknown keys are
// absent.
func (b *Builder) lookupMetrics(ctx context.Context, keys []string) (map[string]metricInfo, error) {
	out := make(map[string]metricInfo, len(keys))
	var custom []string
	for _, k := range keys {
		if m, ok := builtinInfo(k); ok {
			out[k] = m
		} else {
			custom = append(custom, k)
		}
	}
	if len(custom) == 0 {
		return out, nil
	}
	var rows []struct {
		Key      string `gorm:"column:key"`
		Name     string `gorm:"column:name"`
		Units    string `gorm:"column:units"`
		Kind     string `gorm:"column:kind"`
		Builtin  bool   `gorm:"column:builtin"`
		Profile  string `gorm:"column:profile"`
		Position int    `gorm:"column:position"`
	}
	if err := b.db.WithContext(ctx).Raw(`SELECT pm.key, pm.name, pm.units, pm.kind, p.builtin, p.name AS profile, pm.position
		FROM profile_metrics pm JOIN metric_profiles p ON p.id = pm.profile_id
		WHERE pm.key IN ?`, custom).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("describing profile metrics: %w", err)
	}
	for _, r := range rows {
		src := sourceCustom
		if r.Builtin {
			src = sourceProfile
		}
		out[r.Key] = metricInfo{Key: r.Key, Label: r.Name, Unit: r.Units, Source: src, Kind: r.Kind,
			order: r.Position, profile: strings.ToLower(r.Profile)}
	}
	return out, nil
}

// checkMetrics refuses an unknown key, and any metric not allowedOn the
// scope, saying why: a text metric, link speed, or a device metric on a
// ports or port_roles scope (which has no devices to give it lines).
func checkMetrics(scopeType string, keys []string, infos map[string]metricInfo) error {
	for _, k := range keys {
		m, ok := infos[k]
		if !ok {
			return &FieldError{Field: "scope_data.metrics", Message: k + " is not a known metric"}
		}
		if m.allowedOn(scopeType) {
			continue
		}
		switch {
		case m.text():
			return &FieldError{Field: "scope_data.metrics", Message: MsgTextMetric}
		case !m.reportable():
			return &FieldError{Field: "scope_data.metrics", Message: "link speed cannot be reported"}
		default:
			return &FieldError{Field: "scope_data.metrics", Message: m.Label + " is a device metric: report it on devices or sites"}
		}
	}
	return nil
}

// deviceScope reports a scope whose lines include devices.
func deviceScope(scopeType string) bool {
	return scopeType == models.ScopeTypeDevices || scopeType == models.ScopeTypeSites
}
