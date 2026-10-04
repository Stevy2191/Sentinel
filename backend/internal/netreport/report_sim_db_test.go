package netreport

import (
	"context"
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

type simReportNotifier struct{}

func (simReportNotifier) SendNotification(context.Context, *notifications.NotificationMessage) error {
	return nil
}

// TestDBSimMetricsReportEndToEnd polls the simulated Cisco 2960X (community
// "cisco") through the starter health profile, then generates a Metrics
// report on that device's CPU through the same path a scheduled run takes
// (ReportGenerator.GenerateAndSaveReport): statistics from the 5-minute
// rollup, the busiest row first, a PDF on disk and a generation recorded.
func TestDBSimMetricsReportEndToEnd(t *testing.T) {
	addr := os.Getenv("SENTINEL_TEST_SNMPSIM")
	if addr == "" {
		t.Skip("SENTINEL_TEST_SNMPSIM not set; start deploy/snmpsim/run.sh")
	}
	host, portStr, ok := strings.Cut(addr, ":")
	if !ok {
		t.Fatalf("SENTINEL_TEST_SNMPSIM=%q does not contain a colon", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("SENTINEL_TEST_SNMPSIM port %q is not a valid integer: %v", portStr, err)
	}
	db := testdb.Open(t)
	ctx := context.Background()
	admin := testdb.NewUser(t, db, true)

	lib := services.NewMIBLibrary(db)
	testdb.Must(t, lib.SyncBuiltins(ctx))
	profiles := services.NewProfileService(db)
	testdb.Must(t, profiles.SeedStarter(ctx))
	testdb.Must(t, profiles.Load(ctx))

	site, cred, deviceID := uuid.New(), uuid.New(), uuid.New()
	testdb.Exec(t, db, `INSERT INTO sites (id, name) VALUES (?, 'Sim')`, site)
	testdb.Exec(t, db, `INSERT INTO snmp_credentials (id, name, version, community) VALUES (?, 'sim', '2c', 'cisco')`, cred)
	testdb.Exec(t, db, `INSERT INTO devices (id, site_id, credential_id, name, host, sys_object_id)
		VALUES (?, ?, ?, 'sim-cisco', ?, '1.3.6.1.4.1.9.1.1208')`, deviceID, site, cred, host)
	var device models.Device
	testdb.Must(t, db.First(&device, "id = ?", deviceID).Error)

	metrics := services.NewMetricsStore(db)
	incidents := services.NewIncidentService(db)
	ports := services.NewPortService(db, metrics, incidents, services.NewSettingsService(db))
	mon := services.NewProfileMonitor(profiles, metrics, incidents, simReportNotifier{}, snmp.GoSNMPClient{}, ports)
	target := snmp.Target{Host: host, Port: uint16(port), Credential: snmp.Credential{Version: "2c", Community: "cisco"},
		Timeout: 2 * time.Second, Retries: 1}
	mon.PollProfiles(ctx, device, target, -1)

	// The poll wrote its samples now, into the 5-minute bucket still
	// filling, which a report never counts. Copy them 15 minutes back so one
	// complete bucket holds the simulator's readings.
	testdb.Exec(t, db, `INSERT INTO metrics.samples (time, series_id, value)
		SELECT time - INTERVAL '15 minutes', series_id, value FROM metrics.samples`)

	// Refresh the rollups after copying samples 15 minutes back, so they
	// land in a complete 5-minute bucket.
	refresh(t, db)

	agg := services.NewReportAggregatorService(db, nil)
	agg.SetNetworkBuilder(NewBuilder(db, metrics))
	renderer, err := services.NewPDFRendererService(t.TempDir())
	testdb.Must(t, err)
	gen := services.NewReportGenerator(db, agg, renderer, nil)

	report := models.Report{
		ID: uuid.New(), UserID: admin, CreatedBy: admin, Name: "Sim CPU",
		ReportType: models.ReportTypeMetrics, ScopeType: models.ScopeTypeDevices,
		ScopeData:     models.ReportScope{DeviceIDs: []uuid.UUID{deviceID}, Metrics: []string{"cisco_cpu_5min"}},
		TimeRangeDays: 1, PeriodKind: models.PeriodRolling,
	}
	testdb.Must(t, report.Validate())
	testdb.Must(t, db.Create(&report).Error)

	out, err := gen.GenerateAndSaveReport(ctx, &report, admin)
	testdb.Must(t, err)

	raw, err := os.ReadFile(out.Path)
	testdb.Must(t, err)
	if !strings.HasPrefix(string(raw), "%PDF-") || len(raw) < 2000 {
		t.Errorf("PDF is %d bytes starting %q; want a real document", len(raw), raw[:min(8, len(raw))])
	}
	n := out.Data.Network
	if n == nil || n.Empty || n.NoData || len(n.Tables) == 0 {
		t.Fatalf("network data = %+v, want a table of the device's CPU", n)
	}
	rows := n.Tables[0].Rows
	// The simulator reports switch 1 at 23% and the second CPU row at 4%.
	if len(rows) != 2 || !strings.Contains(rows[0].Name, "Switch 1") || rows[0].In == nil || rows[0].In.Avg != 23 ||
		rows[1].In == nil || rows[1].In.Avg != 4 {
		t.Errorf("CPU rows = %+v, want Switch 1 at 23 ranked above the second row at 4", rows)
	}
	var generations int64
	testdb.Must(t, db.Model(&models.ReportGeneration{}).Where("report_id = ?", report.ID).Count(&generations).Error)
	if generations != 1 {
		t.Errorf("%d generations recorded, want 1", generations)
	}
}
