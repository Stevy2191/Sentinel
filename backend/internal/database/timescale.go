package database

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// errTimescaleMissing is what an install sees when its database has no
// TimescaleDB. Written for the person reading the startup log: most often
// someone who pulled a new backend but kept an old docker-compose.yml, whose
// postgres service is still plain postgres:16-alpine.
var errTimescaleMissing = errors.New(
	"Sentinel now requires the TimescaleDB extension. Update docker-compose.yml " +
		"from the current release (the postgres service uses the " +
		"timescale/timescaledb image) and run `docker compose up -d`. " +
		"See GETTING_STARTED.md.")

// TimescaleMissingError returns nil when the extension is available, and the
// explanation otherwise. Kept separate from the query so the message is tested
// without a database.
func TimescaleMissingError(available bool) error {
	if available {
		return nil
	}
	return errTimescaleMissing
}

// RequireTimescale checks, before any migration runs, that the server can load
// TimescaleDB. Without this, migration 044 fails with a bare SQL error that
// says nothing about the compose file.
func RequireTimescale(db *gorm.DB) error {
	var n int64
	if err := db.Raw(
		"SELECT count(*) FROM pg_available_extensions WHERE name = 'timescaledb'",
	).Scan(&n).Error; err != nil {
		return fmt.Errorf("checking for the TimescaleDB extension: %w", err)
	}
	return TimescaleMissingError(n > 0)
}
