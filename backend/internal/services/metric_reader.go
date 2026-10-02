package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Stevy2191/Sentinel/backend/internal/custommetric"
	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

// ErrInvalidMetric wraps a PreviewMetric validation failure, so the
// metric-preview route can return 400 for it rather than the 200 {ok:false}
// reserved for an SNMP or target failure.
var ErrInvalidMetric = errors.New("invalid metric")

// ValueOf converts one SNMP value for metric evaluation.
func ValueOf(p snmp.PDU) custommetric.Value {
	switch v := p.Value.(type) {
	case int64:
		return custommetric.Value{Num: float64(v), NumOK: true}
	case uint64:
		return custommetric.Value{Num: float64(v), NumOK: true}
	case string:
		return custommetric.Value{Text: v}
	case []byte:
		for _, r := range string(v) {
			if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
				parts := make([]string, len(v))
				for i, b := range v {
					parts[i] = fmt.Sprintf("%02x", b)
				}
				return custommetric.Value{Text: strings.Join(parts, ":")}
			}
		}
		return custommetric.Value{Text: snmp.Clean(string(v))}
	}
	return custommetric.Value{}
}

// ReadColumns reads scalars (OIDs ending ".0", one GET) and walks every other
// OID, returning what answered and each OID's error. A walk error does not
// stop the others; an OID that answers with nothing maps to an empty column.
func ReadColumns(ctx context.Context, c snmp.Client, t snmp.Target, oids []string) (custommetric.Columns, map[string]error) {
	cols := custommetric.Columns{}
	errs := map[string]error{}
	var scalars []string
	for _, o := range oids {
		if strings.HasSuffix(o, ".0") {
			scalars = append(scalars, o)
			continue
		}
		pdus, err := c.Walk(ctx, t, o)
		if err != nil {
			errs[o] = err
			continue
		}
		col := map[string]custommetric.Value{}
		for _, p := range pdus {
			if idx, ok := strings.CutPrefix(p.OID, o+"."); ok {
				col[idx] = ValueOf(p)
			}
		}
		cols[o] = col
	}
	if len(scalars) > 0 {
		pdus, err := snmp.GetEach(ctx, c, t, scalars)
		if err != nil {
			for _, o := range scalars {
				errs[o] = err
			}
		}
		for _, p := range pdus {
			if v := ValueOf(p); v.NumOK || v.Text != "" {
				cols[p.OID] = map[string]custommetric.Value{"": v}
			}
		}
	}
	return cols, errs
}

// PreviewRow is one row of a metric preview: a labeled instance with its
// current value, state (for a status metric) and whether it violates the
// metric's alert rule right now.
type PreviewRow struct {
	Instance string  `json:"instance"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	State    string  `json:"state,omitempty"`
	OK       bool    `json:"ok"`
	Violates bool    `json:"violates"`
}

// MetricPreview is what previewing a metric against a live device returns:
// its rows, any per-OID read errors, and whether it is a counter (shown as
// a raw value here since a rate needs two polls to compute).
type MetricPreview struct {
	Rows       []PreviewRow      `json:"rows"`
	Errors     map[string]string `json:"errors"`
	RawCounter bool              `json:"raw_counter"`
}

// PreviewMetric evaluates a metric definition against a device right now,
// for the metric editor's "preview" button. It validates the same way a
// saved metric would (a copy with a placeholder key, since preview happens
// before a key is chosen), then reads and evaluates it like a real poll.
func PreviewMetric(ctx context.Context, c snmp.Client, t snmp.Target, m models.ProfileMetric) (*MetricPreview, error) {
	check := m
	check.Key = "preview_metric"
	if err := ValidateMetric(check); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidMetric, err)
	}
	d := ToDefinition(m)
	every, cached := custommetric.Needed(d)
	cols, errs := ReadColumns(ctx, c, t, append(every, cached...))
	out := &MetricPreview{Rows: []PreviewRow{}, Errors: map[string]string{}, RawCounter: m.Kind == "counter"}
	for oid, err := range errs {
		out.Errors[oid] = err.Error()
	}
	for _, r := range custommetric.Evaluate(d, cols) {
		out.Rows = append(out.Rows, PreviewRow{Instance: r.Instance, Label: r.Label, Value: r.Value, State: r.State, OK: r.OK,
			Violates: d.Rule.Kind != "" && custommetric.Violates(d.Rule, d.Kind, r)})
	}
	return out, nil
}

// DeviceWalker runs a MIB browser "test walk" against a device: it resolves
// the device to an SNMP target (decrypting its credential, applying the same
// resolve/netguard check as every other probe) and walks the given OID
// through the MIB library.
type DeviceWalker struct {
	lib    *MIBLibrary
	prober *Prober
	client snmp.Client
}

// NewDeviceWalker builds a DeviceWalker. The prober already holds the
// credential service, so TestWalkDevice needs nothing beyond the device and
// the OID to walk.
func NewDeviceWalker(lib *MIBLibrary, prober *Prober, client snmp.Client) *DeviceWalker {
	return &DeviceWalker{lib: lib, prober: prober, client: client}
}

// TestWalkDevice resolves d's SNMP target and walks oid.
func (w *DeviceWalker) TestWalkDevice(ctx context.Context, d *DeviceView, oid string) (*TestWalkResult, error) {
	target, err := w.prober.TargetFor(ctx, d.Device)
	if err != nil {
		return nil, err
	}
	return w.lib.TestWalk(ctx, w.client, target, oid)
}

// PreviewDevice resolves d's SNMP target the same way TestWalkDevice does,
// then evaluates m against it right now.
func (w *DeviceWalker) PreviewDevice(ctx context.Context, d *DeviceView, m models.ProfileMetric) (*MetricPreview, error) {
	target, err := w.prober.TargetFor(ctx, d.Device)
	if err != nil {
		return nil, err
	}
	return PreviewMetric(ctx, w.client, target, m)
}
