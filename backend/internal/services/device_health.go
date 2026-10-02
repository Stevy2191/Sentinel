package services

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/custommetric"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
)

// DeviceHealthView is a device's Health tab: every profile's standing with
// it, and the live rows of each applicable metric that has any.
type DeviceHealthView struct {
	Profiles []DeviceProfileView `json:"profiles"`
	Metrics  []HealthMetric      `json:"metrics"`
}

// HealthMetric is one metric's live rows. Rule describes its alert rule in
// words ("above 90 % for 10 min", "not OK"); "" when it has none.
type HealthMetric struct {
	Key   string      `json:"key"`
	Name  string      `json:"name"`
	Kind  string      `json:"kind"`
	Units string      `json:"units"`
	Rule  string      `json:"rule"`
	Rows  []HealthRow `json:"rows"`
}

// HealthRow is one row's latest value. For a status metric State is the
// state's name and OK whether it is an OK state; otherwise OK says the value
// is inside the rule. Problem: an incident is open for the row.
type HealthRow struct {
	Instance string  `json:"instance"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	State    string  `json:"state,omitempty"`
	OK       bool    `json:"ok"`
	Problem  bool    `json:"problem"`
}

// ruleText describes a metric's enabled rule; "" when it has none.
func ruleText(m models.ProfileMetric) string {
	if !hasRule(m) {
		return ""
	}
	if m.RuleKind == "not_ok" {
		return "not OK"
	}
	v := 0.0
	if m.RuleValue != nil {
		v = *m.RuleValue
	}
	out := m.RuleKind + " " + withUnits(v, m.Units)
	if m.RuleHoldMinutes > 0 {
		out += " for " + strconv.Itoa(m.RuleHoldMinutes) + " min"
	}
	return out
}

// healthRow turns a stored sample back into a row: a status metric's sample
// is its state code.
func healthRow(def custommetric.Definition, instance, label string, value float64) HealthRow {
	if label == "" {
		label = instance
	}
	r := HealthRow{Instance: instance, Label: label, Value: value, OK: true}
	if def.Kind == "status" {
		code := int64(value)
		r.State = def.StateNames[code]
		if r.State == "" {
			r.State = strconv.FormatInt(code, 10)
		}
		r.OK = slices.Contains(def.OKStates, code)
		return r
	}
	if def.Rule.Enabled && def.Rule.Kind != "" {
		r.OK = !custommetric.Violates(def.Rule, def.Kind, custommetric.Row{Instance: instance, Value: value})
	}
	return r
}

// DeviceHealth returns the device's Health view: profile standings and the
// latest value of every applicable metric's rows within the live window
// (three runs of the longest applicable profile interval or of the device's
// poll interval, whichever is longer, since a profile cannot run more often
// than its device is polled; at least five minutes), labelled from
// metrics.series.
func (s *ProfileService) DeviceHealth(ctx context.Context, d *DeviceView, metrics *MetricsStore, incidents *IncidentService) (*DeviceHealthView, error) {
	views, err := s.DeviceProfiles(ctx, d.Device)
	if err != nil {
		return nil, err
	}
	applicable, err := s.ProfilesForDevice(ctx, d.Device)
	if err != nil {
		return nil, err
	}
	out := &DeviceHealthView{Profiles: views, Metrics: []HealthMetric{}}
	longest := 0
	var keys []string
	for _, p := range applicable {
		longest = max(longest, p.Profile.PollIntervalMinutes)
		for _, m := range p.Metrics {
			keys = append(keys, m.Key)
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	latest, err := metrics.LatestMany(ctx, []uuid.UUID{d.ID}, keys, liveSince(time.Now(), max(d.PollInterval, longest*60)))
	if err != nil {
		return nil, err
	}
	labels, err := metrics.SeriesLabels(ctx, d.ID, keys)
	if err != nil {
		return nil, err
	}
	open, err := incidents.OpenMetricIncidents(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	problem := map[string]bool{}
	for _, inc := range open {
		if inc.MetricKey != nil && inc.MetricInstance != nil {
			problem[*inc.MetricKey+"|"+*inc.MetricInstance] = true
		}
	}
	for _, p := range applicable {
		for _, m := range p.Metrics {
			values := latest[d.ID][m.Key]
			if len(values) == 0 {
				continue
			}
			def := ToDefinition(m)
			hm := HealthMetric{Key: m.Key, Name: m.Name, Kind: m.Kind, Units: m.Units, Rule: ruleText(m)}
			for inst, v := range values {
				row := healthRow(def, inst, labels[m.Key][inst], v)
				row.Problem = problem[m.Key+"|"+inst]
				hm.Rows = append(hm.Rows, row)
			}
			sort.Slice(hm.Rows, func(i, j int) bool { return custommetric.LessIndex(hm.Rows[i].Instance, hm.Rows[j].Instance) })
			out.Metrics = append(out.Metrics, hm)
		}
	}
	return out, nil
}
