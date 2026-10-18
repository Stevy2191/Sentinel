package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type fixedBays string

func (b fixedBays) State(uuid.UUID) string { return string(b) }

// A drawn device with a drive bay gets the bay's state; any other gets no
// indicators. Both carry the device's reachability.
func TestDevicePortsIndicators(t *testing.T) {
	f := newEditorFixture(t)
	f.ports.SetDriveBays(fixedBays(DriveBayActive))
	ctx := context.Background()
	udm := seedModelDevice(t, f.db, "10.3.0.1", "UDM-SE")
	testdb.Exec(t, f.db, `UPDATE devices SET sys_descr = 'Ubiquiti UniFi UDM-SE 5.1.33 Linux', status = 'up' WHERE id = ?`, udm)
	for i := 0; i <= 10; i++ {
		testdb.Exec(t, f.db, `INSERT INTO device_interfaces (device_id, if_index, name, descr, if_type) VALUES (?, ?, ?, ?, 6)`,
			udm, 16+i, fmt.Sprintf("eth%d", i), fmt.Sprintf("eth%d", i))
	}
	v, err := f.ports.DevicePorts(ctx, f.view(t, udm))
	testdb.Must(t, err)
	if v.Faceplates[0].Drawing != "udm-se" || v.Indicators == nil || v.Indicators.DriveBay != DriveBayActive {
		t.Errorf("UDM-SE: drawing %q indicators %+v", v.Faceplates[0].Drawing, v.Indicators)
	}
	if v.DeviceState.Status != models.DeviceStatusUp || v.DeviceState.PollInterval == 0 {
		t.Errorf("device state %+v", v.DeviceState)
	}

	sw := seedModelDevice(t, f.db, "10.3.0.2", "USW-24")
	seedPorts(t, f.db, sw, 0, 24)
	v, err = f.ports.DevicePorts(ctx, f.view(t, sw))
	testdb.Must(t, err)
	if v.Indicators != nil {
		t.Errorf("USW-24 indicators %+v", v.Indicators)
	}
}
