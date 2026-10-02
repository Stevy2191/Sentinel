package dashboards

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Stevy2191/Sentinel/backend/internal/models"
	"github.com/Stevy2191/Sentinel/backend/internal/notifications"
	"github.com/Stevy2191/Sentinel/backend/internal/services"
	"github.com/Stevy2191/Sentinel/backend/internal/snmp"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

type simNotifier struct{}

func (simNotifier) SendNotification(context.Context, *notifications.NotificationMessage) error {
	return nil
}

func TestDBSimWidgetsEndToEnd(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, _ := strings.Cut(addr, ":")
	port, _ := strconv.Atoi(portStr)
	target := func(community string) snmp.Target {
		return snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: community}, Timeout: 2 * time.Second, Retries: 1}
	}
	db := testdb.Open(t)
	ctx := context.Background()
	d := realDeps(t, db)
	site := newSite(t, db, "Sim")

	edge := newDevice(t, db, site, "sim-edge", host)
	inv, err := snmp.ReadInventory(ctx, snmp.GoSNMPClient{}, target("edgeswitch"))
	testdb.Must(t, err)
	testdb.Must(t, d.Devices.(*services.DeviceService).SaveInventory(ctx, edge, inv, time.Now()))
	data, err := resolveWidget(t, portGridWidget{devices: d.Devices, ports: d.Ports}, fmt.Sprintf(`{"device_id":"%s"}`, edge),
		ResolveInput{Visible: Subjects{Devices: []uuid.UUID{edge}}})
	testdb.Must(t, err)
	if pg := data.(PortGridData); len(pg.Ports) == 0 || len(pg.Faceplates) == 0 || pg.Ports[0].Name == "" {
		t.Errorf("port grid from the simulated EdgeSwitch = %d ports, %d faceplates", len(pg.Ports), len(pg.Faceplates))
	}

	lib := services.NewMIBLibrary(db)
	testdb.Must(t, lib.SyncBuiltins(ctx))
	profiles := services.NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))
	cisco := newDevice(t, db, newSite(t, db, "Sim Cisco"), "sim-cisco", host) // own site: same host:port as the edge switch
	testdb.Exec(t, db, `UPDATE devices SET sys_object_id = '1.3.6.1.4.1.9.1.1208' WHERE id = ?`, cisco)
	var dev models.Device
	testdb.Must(t, db.First(&dev, "id = ?", cisco).Error)
	mon := services.NewProfileMonitor(profiles, d.Metrics.(*services.MetricsStore), services.NewIncidentService(db), simNotifier{},
		snmp.GoSNMPClient{}, d.Ports.(*services.PortService))
	mon.PollProfiles(ctx, dev, target("cisco"), -1)

	in := ResolveInput{Visible: Subjects{Devices: []uuid.UUID{cisco}}}
	data, err = resolveWidget(t, timeseriesWidget{metrics: d.Metrics, ports: d.Ports, devices: d.Devices},
		fmt.Sprintf(`{"metrics":["cisco_cpu_5min"],"devices":["%s"],"instances":["1"],"range":"1h"}`, cisco), in)
	testdb.Must(t, err)
	ts := data.(TimeseriesData)
	if len(ts.Lines) != 1 || len(ts.Lines[0].Points) == 0 || ts.Lines[0].Points[len(ts.Lines[0].Points)-1].Avg != 23 || !strings.Contains(ts.Lines[0].Label, "Switch 1") {
		t.Errorf("CPU of switch 1 = %+v, want the simulator's 23 labelled Switch 1", ts.Lines)
	}
	data, err = resolveWidget(t, deviceHealthWidget{devices: d.Devices, health: d.Health}, fmt.Sprintf(`{"device_id":"%s"}`, cisco), in)
	testdb.Must(t, err)
	if len(data.(DeviceHealthData).Metrics) == 0 {
		t.Error("device health from the simulated Cisco has no metrics")
	}
}
