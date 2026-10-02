package services

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/Stevy2191/Sentinel/backend/internal/custommetric"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
)

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
