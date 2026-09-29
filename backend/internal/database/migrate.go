package database

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"gorm.io/gorm"
)

// RunMigrations applies any *.sql files in dir that have not yet been recorded
// in the schema_migrations table, in filename order.
func RunMigrations(db *gorm.DB, dir string) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename   TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`).Error; err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("listing migrations in %q: %w", dir, err)
	}
	sort.Strings(files)

	for _, path := range files {
		name := filepath.Base(path)

		var applied int64
		if err := db.Raw("SELECT count(*) FROM schema_migrations WHERE filename = ?", name).Scan(&applied).Error; err != nil {
			return fmt.Errorf("checking migration %q: %w", name, err)
		}
		if applied > 0 {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading migration %q: %w", name, err)
		}
		log.Printf("applying migration %s", name)
		if err := db.Exec(string(content)).Error; err != nil {
			return fmt.Errorf("applying migration %q: %w", name, err)
		}
		if err := db.Exec("INSERT INTO schema_migrations (filename) VALUES (?)", name).Error; err != nil {
			return fmt.Errorf("recording migration %q: %w", name, err)
		}
	}
	return nil
}
