package database

import (
	"errors"
	"fmt"
	"strings"

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

// errTimescaleNotPreloaded is the partial upgrade: the image is right but the
// postgres service's command block, which preloads the extension, is missing.
var errTimescaleNotPreloaded = errors.New(
	"Sentinel's database has TimescaleDB but does not load it. The postgres service in " +
		"docker-compose.yml needs the command lines that pass " +
		"shared_preload_libraries=timescaledb (copy the whole postgres service from the " +
		"current release), then run `docker compose up -d`. See GETTING_STARTED.md.")

// TimescaleStatus is what the preflight learns from the server.
type TimescaleStatus struct {
	// Available: the server has the extension installed (the image is right).
	Available bool
	// Preloaded: the server loads it at startup (the compose command is right).
	Preloaded bool
}

// TimescaleMissingError returns nil when TimescaleDB is usable, and otherwise
// the explanation for whichever half of the upgrade is missing. Kept separate
// from the queries so the messages are tested without a database.
func TimescaleMissingError(st TimescaleStatus) error {
	switch {
	case !st.Available:
		return errTimescaleMissing
	case !st.Preloaded:
		return errTimescaleNotPreloaded
	default:
		return nil
	}
}

// RequireTimescale checks, before any migration runs, that the server can load
// TimescaleDB. Without this, migration 044 fails with a bare SQL error that
// says nothing about the compose file.
func RequireTimescale(db *gorm.DB) error {
	var st TimescaleStatus
	if err := db.Raw(
		"SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb')",
	).Scan(&st.Available).Error; err != nil {
		return fmt.Errorf("checking for the TimescaleDB extension: %w", err)
	}
	var preload string
	if err := db.Raw("SELECT current_setting('shared_preload_libraries')").Scan(&preload).Error; err != nil {
		return fmt.Errorf("checking shared_preload_libraries: %w", err)
	}
	st.Preloaded = strings.Contains(preload, "timescaledb")
	return TimescaleMissingError(st)
}
