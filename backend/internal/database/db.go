// Package database provides PostgreSQL connectivity, connection pooling, and
// migration helpers for Sentinel, built on GORM.
package database

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Connection-pool defaults. These keep resource usage predictable while
// leaving headroom for concurrent monitor checks.
const (
	maxOpenConns    = 25
	maxIdleConns    = 5
	connMaxLifetime = 5 * time.Minute
)

// Startup connection retry. The database is often not ready when the backend
// starts: on a fresh install Postgres restarts once after initialising its
// data directory, and after a host reboot Docker starts containers in no
// particular order, so the "postgres" name may not even resolve yet. Rather
// than exit on the first failure and rely on the restart policy, NewDB keeps
// trying for connectWindow, doubling the wait between attempts up to
// connectMaxDelay.
const (
	connectWindow   = 60 * time.Second
	connectMaxDelay = 10 * time.Second
)

// Database wraps a GORM connection together with a logger, exposing lifecycle,
// health, and migration helpers for the rest of the application.
type Database struct {
	db     *gorm.DB
	logger *log.Logger
}

// NewDB opens a PostgreSQL connection using GORM and configures connection
// pooling. Configuration values are taken from the provided map when present,
// otherwise from the corresponding environment variables (DB_HOST, DB_PORT,
// DB_NAME, DB_USER, DB_PASSWORD, DB_SSL_MODE), with sensible development
// defaults. A database that is not reachable yet is retried for up to
// connectWindow, logging each failed attempt. It returns the *gorm.DB handle
// or an error if no connection could be established in that time.
func NewDB(config map[string]string) (*gorm.DB, error) {
	host := lookup(config, "DB_HOST", "localhost")
	port := lookup(config, "DB_PORT", "5432")
	name := lookup(config, "DB_NAME", "sentinel")
	user := lookup(config, "DB_USER", "sentinel")
	password := lookup(config, "DB_PASSWORD", "")
	sslMode := lookup(config, "DB_SSL_MODE", "disable")

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		host, port, user, password, name, sslMode,
	)

	// gorm.Open pings the server, so an unreachable database fails here (and
	// gorm closes the pool it opened) rather than on the first query.
	open := func() (*gorm.DB, error) {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	}
	log.Printf("[database] connecting to postgres %s:%s/%s", host, port, name)
	db, err := connectRetry(open, connectWindow, time.Now, time.Sleep)
	if err != nil {
		return nil, fmt.Errorf("opening postgres connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("accessing underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

	log.Printf("[database] connected to postgres %s:%s/%s (sslmode=%s)", host, port, name, sslMode)
	return db, nil
}

// connectRetry calls open until it succeeds or window has passed, waiting
// 1s, 2s, 4s... (at most connectMaxDelay) between attempts. A server that
// answers and refuses the login is not retried (see refusedLogin). The clock
// and sleep are parameters so tests can run the window without waiting it out.
func connectRetry(open func() (*gorm.DB, error), window time.Duration,
	now func() time.Time, sleep func(time.Duration)) (*gorm.DB, error) {
	deadline := now().Add(window)
	delay := time.Second
	for attempt := 1; ; attempt++ {
		db, err := open()
		if err == nil {
			return db, nil
		}
		if refusedLogin(err) {
			return nil, err
		}
		remaining := deadline.Sub(now())
		if remaining <= 0 {
			return nil, fmt.Errorf("database still unreachable after %s (%d attempts): %w", window, attempt, err)
		}
		wait := min(delay, remaining)
		log.Printf("[database] attempt %d failed: %v; retrying in %s", attempt, err, wait)
		sleep(wait)
		delay = min(delay*2, connectMaxDelay)
	}
}

// refusedLogin reports whether err is the server rejecting the credentials
// (SQLSTATE class 28: wrong password, unknown role) or naming a database that
// does not exist (3D000). Those are configuration errors that waiting will
// not fix, so they surface at once rather than after the whole retry window.
// pgx's *pgconn.PgError reports its code through SQLState.
func refusedLogin(err error) bool {
	var pgErr interface{ SQLState() string }
	if !errors.As(err, &pgErr) {
		return false
	}
	code := pgErr.SQLState()
	return strings.HasPrefix(code, "28") || code == "3D000"
}

// New opens a connection via NewDB and returns a Database wrapper with an
// attached logger, on which Close, Health, and AutoMigrate can be called. If
// logger is nil, the standard logger is used.
func New(config map[string]string, logger *log.Logger) (*Database, error) {
	if logger == nil {
		logger = log.Default()
	}
	db, err := NewDB(config)
	if err != nil {
		return nil, err
	}
	return &Database{db: db, logger: logger}, nil
}

// DB returns the wrapped *gorm.DB handle for use by repositories and services.
func (d *Database) DB() *gorm.DB {
	return d.db
}

// Close gracefully closes the underlying database connection pool.
func (d *Database) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("accessing underlying sql.DB: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("closing database connection: %w", err)
	}
	d.logger.Println("[database] connection closed")
	return nil
}

// Health verifies connectivity by pinging the database. It returns an error if
// the database is unreachable.
func (d *Database) Health() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return fmt.Errorf("accessing underlying sql.DB: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("database unreachable: %w", err)
	}
	return nil
}

// AutoMigrate creates or updates the tables backing the provided models so the
// schema matches their current definitions. Passing no models is a no-op.
func (d *Database) AutoMigrate(models ...any) error {
	if len(models) == 0 {
		return nil
	}
	if err := d.db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	d.logger.Printf("[database] auto-migrated %d model(s)", len(models))
	return nil
}

// lookup returns the value for key from config, falling back to the environment
// variable of the same name, then to fallback.
func lookup(config map[string]string, key, fallback string) string {
	if config != nil {
		if v, ok := config[key]; ok && v != "" {
			return v
		}
	}
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
