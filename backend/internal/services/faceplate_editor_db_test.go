package services

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// seedPorts adds Ethernet ports 1..n named "0/N" (or "U/0/N" with a stack
// unit), skipping the numbers in skip.
func seedPorts(t *testing.T, db *gorm.DB, dev uuid.UUID, unit, n int, skip ...int) {
	t.Helper()
	skipped := map[int]bool{}
	for _, s := range skip {
		skipped[s] = true
	}
	for i := 1; i <= n; i++ {
		if skipped[i] {
			continue
		}
		name := fmt.Sprintf("0/%d", i)
		if unit > 0 {
			name = fmt.Sprintf("%d/0/%d", unit, i)
		}
		testdb.Exec(t, db, `INSERT INTO device_interfaces (device_id, if_index, name, descr, if_type, stack_unit)
			VALUES (?, ?, ?, ?, 6, ?)`, dev, unit*1000+i, name, fmt.Sprintf("Slot: 0 Port: %d 10G - Level", i), unit)
	}
}

type editorFixture struct {
	db      *gorm.DB
	ports   *PortService
	layouts *FaceplateLayoutService
	devices *DeviceService
	admin   uuid.UUID
}

func newEditorFixture(t *testing.T) editorFixture {
	db := testdb.Open(t)
	layouts := NewFaceplateLayoutService(db)
	ports := NewPortService(db, NewMetricsStore(db), NewIncidentService(db), NewSettingsService(db))
	ports.SetFaceplateLayouts(layouts)
	return editorFixture{db: db, ports: ports, layouts: layouts, devices: NewDeviceService(db, NewSNMPCredentialService(db), NewIncidentService(db)), admin: testdb.NewUser(t, db, true)}
}

func (f editorFixture) view(t *testing.T, id uuid.UUID) *DeviceView {
	t.Helper()
	d, err := f.devices.Get(context.Background(), id)
	testdb.Must(t, err)
	return d
}

func oneRJ45Row(n int) []portmon.LayoutGroup {
	return []portmon.LayoutGroup{{First: 1, Last: n, Kind: portmon.GroupKindRJ45, Rows: 1}}
}

func TestDevicePortsUsesSavedLayout(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.1.0.1", "UBNT-ES16XG")
	seedPorts(t, f.db, dev, 0, 16)
	_, _, err := f.layouts.Save(ctx, "UBNT-ES16XG", oneRJ45Row(16), f.admin, nil)
	testdb.Must(t, err)

	v, err := f.ports.DevicePorts(ctx, f.view(t, dev))
	testdb.Must(t, err)
	if b := v.Faceplates[0].Blocks; len(b) != 1 || b[0].SFP || len(b[0].Top) != 16 {
		t.Errorf("saved layout not used: %+v", v.Faceplates)
	}
	if v.LayoutNotice != nil {
		t.Errorf("notice on a fitting layout: %+v", v.LayoutNotice)
	}
}

// A stack whose second member lacks a port: unit 1 uses the layout, unit 2
// is drawn automatically, and admins are told why.
func TestDevicePortsSavedLayoutStackMisfit(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.1.0.2", "USW-STACK")
	seedPorts(t, f.db, dev, 1, 16)
	seedPorts(t, f.db, dev, 2, 16, 16)
	_, _, err := f.layouts.Save(ctx, "USW-STACK", oneRJ45Row(16), f.admin, nil)
	testdb.Must(t, err)

	v, err := f.ports.DevicePorts(ctx, f.view(t, dev))
	testdb.Must(t, err)
	if len(v.Faceplates) != 2 {
		t.Fatalf("faceplates %+v", v.Faceplates)
	}
	if b := v.Faceplates[0].Blocks; len(b) != 1 || b[0].Rows != 1 {
		t.Errorf("unit 1 should use the saved layout: %+v", b)
	}
	if b := v.Faceplates[1].Blocks; len(b) == 0 || b[0].Rows != 0 {
		t.Errorf("unit 2 should be automatic: %+v", b)
	}
	if v.LayoutNotice == nil || v.LayoutNotice.Model != "USW-STACK" || !reflect.DeepEqual(v.LayoutNotice.Fit.Extra, []int{16}) {
		t.Errorf("notice %+v", v.LayoutNotice)
	}
}

func TestDevicePortsNoticeHiddenByDeviceSettings(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.1.0.3", "USW-24")
	seedPorts(t, f.db, dev, 0, 15)
	testdb.Exec(t, f.db, `UPDATE devices SET faceplate_rows = 2 WHERE id = ?`, dev)
	_, _, err := f.layouts.Save(ctx, "USW-24", oneRJ45Row(16), f.admin, nil)
	testdb.Must(t, err)
	v, err := f.ports.DevicePorts(ctx, f.view(t, dev))
	testdb.Must(t, err)
	if v.LayoutNotice != nil {
		t.Errorf("notice although the device's own settings apply: %+v", v.LayoutNotice)
	}
}

func TestFaceplateEditorStart(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.1.0.4", "USW-Pro-48")
	for i := 1; i <= 48; i++ {
		testdb.Exec(t, f.db, `INSERT INTO device_interfaces (device_id, if_index, name, descr, if_type) VALUES (?, ?, ?, ?, 6)`,
			dev, i, fmt.Sprintf("0/%d", i), fmt.Sprintf("Slot: 0 Port: %d Gigabit - Level", i))
	}
	for i := 49; i <= 52; i++ {
		testdb.Exec(t, f.db, `INSERT INTO device_interfaces (device_id, if_index, name, descr, if_type) VALUES (?, ?, ?, ?, 6)`,
			dev, i, fmt.Sprintf("0/%d", i), fmt.Sprintf("Slot: 0 Port: %d 10G - Level", i))
	}
	e, err := f.ports.FaceplateEditor(ctx, f.view(t, dev))
	testdb.Must(t, err)
	if e.Model != "USW-Pro-48" || e.Saved != nil || e.Builtin != nil || len(e.Start) != 5 || len(e.Ports) != 52 || e.DeviceSettings {
		t.Fatalf("editor %+v", e)
	}

	saved := []portmon.LayoutGroup{{First: 1, Last: 52, Kind: portmon.GroupKindRJ45, Rows: 2, Numbering: portmon.NumberingAcross}}
	_, _, err = f.layouts.Save(ctx, "USW-Pro-48", saved, f.admin, nil)
	testdb.Must(t, err)
	testdb.Exec(t, f.db, `UPDATE devices SET faceplate_port_style = 'sfp' WHERE id = ?`, dev)
	e, err = f.ports.FaceplateEditor(ctx, f.view(t, dev))
	testdb.Must(t, err)
	if e.Saved == nil || !reflect.DeepEqual(e.Start, saved) || !e.DeviceSettings {
		t.Errorf("editor with a saved layout %+v", e)
	}
}

func TestPreviewFaceplate(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.1.0.5", "UBNT-ES16XG")
	seedPorts(t, f.db, dev, 0, 16)
	testdb.Exec(t, f.db, `UPDATE devices SET faceplate_rows = 1 WHERE id = ?`, dev)
	d := f.view(t, dev)

	if _, err := f.ports.PreviewFaceplate(ctx, d, []portmon.LayoutGroup{{First: 1, Last: 4, Rows: 1}}); !errors.Is(err, ErrFaceplateGroupsInvalid) {
		t.Errorf("invalid groups: %v", err)
	}
	p, err := f.ports.PreviewFaceplate(ctx, d, oneRJ45Row(15))
	testdb.Must(t, err)
	if !reflect.DeepEqual(p.Fit.Missing, []int{16}) {
		t.Errorf("fit %+v", p.Fit)
	}
	two := []portmon.LayoutGroup{{First: 1, Last: 16, Kind: portmon.GroupKindRJ45, Rows: 2, Numbering: portmon.NumberingAlternate}}
	p, err = f.ports.PreviewFaceplate(ctx, d, two)
	testdb.Must(t, err)
	if !p.Fit.OK() || p.Faceplates[0].Rows != 2 || len(p.Faceplates[0].Blocks[0].Bottom) != 8 {
		t.Errorf("preview should ignore the device's one-row setting: %+v", p)
	}
}

// With no saved or built-in layout, the editor starts from the drawing the
// device shows now, its own Edit details settings included.
func TestFaceplateEditorStartUsesDeviceSettings(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.1.0.6", "USW-16")
	seedPorts(t, f.db, dev, 0, 16) // "10G - Level": automatic draws SFP
	testdb.Exec(t, f.db, `UPDATE devices SET faceplate_port_style = 'rj45' WHERE id = ?`, dev)
	e, err := f.ports.FaceplateEditor(ctx, f.view(t, dev))
	testdb.Must(t, err)
	if len(e.Start) == 0 || e.Start[0].Kind != portmon.GroupKindRJ45 {
		t.Errorf("start %+v, want the device's RJ45 drawing", e.Start)
	}
}
