// Package database provides PostgreSQL connectivity, connection pooling, and
// migration helpers for Sentinel, built on GORM.
package database

import (
	"fmt"
	"log"
	"os"
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
// defaults. It returns the *gorm.DB handle or an error if the connection
// cannot be established.
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

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
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
