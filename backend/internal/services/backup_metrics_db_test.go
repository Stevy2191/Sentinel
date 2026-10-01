package services

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// A config backup restored over a database whose metrics schema holds
// series and samples must succeed, and leave the metrics in place. Runs
// pg_dump and psql inside the test container with the backup's own
// arguments, since the host has no postgres client tools.
func TestDBRestoreWithMetrics(t *testing.T) {
	container := os.Getenv("SENTINEL_TEST_DB_CONTAINER")
	if container == "" {
		t.Skip("SENTINEL_TEST_DB_CONTAINER not set; run `make test-db`")
	}
	db := testdb.Open(t)
	s := seedDevice(t, db, "HQ", "10.0.0.9")
	ctx := context.Background()
	testdb.Must(t, NewMetricsStore(db).Write(ctx, s.DeviceID, time.Now().UTC(),
		[]SamplePoint{{Metric: MetricIfInBps, Instance: "1", Value: 5}}))

	var name string
	testdb.Must(t, db.Raw("SELECT current_database()").Scan(&name).Error)
	b := &BackupService{db: DBConfig{Host: "127.0.0.1", Port: "5432", User: "sentinel", Name: name}}

	dump, err := exec.Command("docker", append([]string{"exec", "-e", "PGPASSWORD=test", container, "pg_dump"}, b.dumpArgs()...)...).Output()
	if err != nil {
		t.Fatalf("pg_dump: %v", err)
	}
	restore := exec.Command("docker", append([]string{"exec", "-i", "-e", "PGPASSWORD=test", container, "psql"}, b.restoreArgs()...)...)
	restore.Stdin = bytes.NewReader(dump)
	if out, err := restore.CombinedOutput(); err != nil {
		t.Fatalf("restore failed: %v\n%s", err, out)
	}

	var devices, series, samples int64
	testdb.Must(t, db.Raw(`SELECT count(*) FROM devices`).Scan(&devices).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.series WHERE device_id = ?`, s.DeviceID).Scan(&series).Error)
	testdb.Must(t, db.Raw(`SELECT count(*) FROM metrics.samples`).Scan(&samples).Error)
	if devices != 1 || series != 1 || samples != 1 {
		t.Errorf("after restore: %d devices, %d series, %d samples; want 1, 1, 1", devices, series, samples)
	}
}
