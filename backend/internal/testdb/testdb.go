// Package testdb gives integration tests a real, migrated PostgreSQL database.
//
// Tests that need one call Open(t). It skips the test unless
// SENTINEL_TEST_DATABASE_URL is set, so plain `go test ./...` stays
// database-free; `make test-db` starts a throwaway TimescaleDB and sets it.
//
// Every call creates a brand-new database and drops it when the test ends, so
// tests never see each other's rows and can run in parallel.
package testdb

import (
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/Stevy2191/Sentinel/backend/internal/database"
)

// EnvURL names the variable holding an admin connection string, e.g.
// postgres://sentinel:test@127.0.0.1:55432/sentinel?sslmode=disable
const EnvURL = "SENTINEL_TEST_DATABASE_URL"

// MigrationsDir is backend/migrations, located from this file so tests work
// from any package directory.
func MigrationsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

// Open returns a fresh database with every migration applied.
func Open(t *testing.T) *gorm.DB {
	t.Helper()
	raw := os.Getenv(EnvURL)
	if raw == "" {
		t.Skipf("%s not set; run `make test-db` for database tests", EnvURL)
	}

	admin, err := gorm.Open(postgres.Open(raw), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connecting to %s: %v", EnvURL, err)
	}
	name := "t_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("creating test database: %v", err)
	}

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parsing %s: %v", EnvURL, err)
	}
	u.Path = "/" + name
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}

	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
		admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := database.RunMigrations(db, MigrationsDir()); err != nil {
		t.Fatalf("migrating test database: %v", err)
	}
	return db
}

// Exec runs a statement and fails the test on error. For fixtures.
func Exec(t *testing.T, db *gorm.DB, sql string, args ...any) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

// NewUser inserts a user and returns its id.
func NewUser(t *testing.T, db *gorm.DB, admin bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	name := "u" + strings.ReplaceAll(id.String()[:8], "-", "")
	role := "user"
	if admin {
		role = "admin"
	}
	Exec(t, db, `INSERT INTO users (id, username, email, password_hash, is_admin, role)
		VALUES (?, ?, ?, 'x', ?, ?)`, id, name, name+"@example.test", admin, role)
	return id
}

// Must fails the test on a non-nil error.
func Must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
