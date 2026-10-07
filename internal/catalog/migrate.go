package catalog

import (
	"database/sql"
	"fmt"

	schemamigrations "github.com/thebrazenbeard/mediaphile-server/migrations"
)

type migration struct {
	version int
	sql     string
}

var migrations = []migration{
	{version: 1, sql: schemamigrations.Initial},
	{version: 2, sql: schemamigrations.KnowledgeWebhooks},
}

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
	version INTEGER PRIMARY KEY,
	applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	for _, m := range migrations {
		var applied int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version=?", m.version).Scan(&applied); err != nil {
			return err
		}
		if applied != 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations(version) VALUES (?)", m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
