package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/portmon"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A UniFi console's eth0-eth10 are ports 1-11 everywhere: the port list,
// the faceplate (the built-in UDM-SE layout), port events and live ports.
func TestDBUniFiConsolePortNumbers(t *testing.T) {
	f := newEditorFixture(t)
	ctx := context.Background()
	dev := seedModelDevice(t, f.db, "10.2.0.1", "UDM-SE")
	testdb.Exec(t, f.db, `UPDATE devices SET sys_descr = 'Ubiquiti UniFi UDM-SE 5.1.33 Linux 4.19.152 al324' WHERE id = ?`, dev)
	ifIDs := map[int]uuid.UUID{}
	for i := 0; i <= 10; i++ {
		id := uuid.New()
		ifIDs[i] = id
		testdb.Exec(t, f.db, `INSERT INTO device_interfaces (id, device_id, if_index, name, descr, if_type) VALUES (?, ?, ?, ?, ?, 6)`,
			id, dev, 16+i, fmt.Sprintf("eth%d", i), fmt.Sprintf("eth%d", i))
	}
	d := f.view(t, dev)

	v, err := f.ports.DevicePorts(ctx, d)
	testdb.Must(t, err)
	for _, p := range v.Ports {
		if want := p.IfIndex - 16 + 1; p.Number != want || p.Label != fmt.Sprint(want) {
			t.Errorf("%s: number %d label %q, want %d", p.Name, p.Number, p.Label, want)
		}
	}
	if b := v.Faceplates[0].Blocks; len(b) != 3 || b[1].Rows != 1 || b[1].Top[0].Number != 9 {
		t.Errorf("faceplate %+v", v.Faceplates)
	}

	testdb.Exec(t, f.db, `INSERT INTO port_events (device_id, interface_id, if_index, kind, started_at) VALUES (?, ?, 25, 'link_down', now())`, dev, ifIDs[9])
	events, _, err := f.ports.Events(ctx, PortEventFilter{DeviceID: &dev})
	testdb.Must(t, err)
	if len(events) != 1 || events[0].PortNumber != 10 || events[0].PortLabel != "10" {
		t.Errorf("events %+v", events)
	}

	live, err := f.ports.LivePorts(ctx, []uuid.UUID{ifIDs[0]})
	testdb.Must(t, err)
	if lp := live[ifIDs[0]]; lp.Number != 1 || lp.Label != "1" {
		t.Errorf("live port %+v", lp)
	}

	e, err := f.ports.FaceplateEditor(ctx, d)
	testdb.Must(t, err)
	if len(e.Ports) != 11 || e.Ports[0] != 1 || len(e.Start) != 3 {
		t.Errorf("editor %+v", e)
	}
	if p, err := f.ports.PreviewFaceplate(ctx, d, []portmon.LayoutGroup{{First: 1, Last: 11, Kind: portmon.GroupKindRJ45, Rows: 1}}); err != nil || !p.Fit.OK() {
		t.Errorf("preview fit %+v %v", p, err)
	}
}
