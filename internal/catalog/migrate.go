package catalog

import (
	"database/sql"
	"fmt"

	schemamigrations "github.com/thebrazenbeard/mediaphile-server/migrations"
)

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	var applied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&applied); err != nil {
		return err
	}
	if applied != 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schemamigrations.Initial); err != nil {
		return fmt.Errorf("apply migration 1: %w", err)
	}
	if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (1)"); err != nil {
		return fmt.Errorf("record migration 1: %w", err)
	}
	return tx.Commit()
}
