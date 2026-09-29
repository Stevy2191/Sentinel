package database_test

import (
	"testing"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
	"github.com/Stevy2191/Sentinel/backend/internal/testdb"
)

// Migrations apply to an empty database, and running them again is a no-op:
// the runner skips what schema_migrations records.
func TestDBMigrationsApplyAndAreIdempotent(t *testing.T) {
	db := testdb.Open(t) // already migrated once
	if err := database.RunMigrations(db, testdb.MigrationsDir()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	var n int64
	testdb.Must(t, db.Raw("SELECT count(*) FROM schema_migrations").Scan(&n).Error)
	if n < 45 {
		t.Errorf("schema_migrations has %d rows, want at least 45", n)
	}
	var ext string
	testdb.Must(t, db.Raw("SELECT extname FROM pg_extension WHERE extname = 'timescaledb'").Scan(&ext).Error)
	if ext != "timescaledb" {
		t.Error("timescaledb extension missing from the test database")
	}
}
