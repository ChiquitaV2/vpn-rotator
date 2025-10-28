package db

import (
	"database/sql"
	"fmt"
	"time"
)

const (
	// Migration version for tracking schema changes
	currentSchemaVersion = 1
)

// Migrations represents a database migration
type Migration struct {
	Version     int
	Description string
	Up          string
	Down        string
}

// GetMigrations returns all available migrations in order
func GetMigrations() []Migration {
	return []Migration{
		{
			Version:     1,
			Description: "Initial schema with nodes table",
			Up:          ddl, // Use the embedded schema
			Down:        `DROP TABLE IF EXISTS nodes; DROP TABLE IF EXISTS schema_migrations;`,
		},
		// Add future migrations here
	}
}

// EnsureMigrationsTable creates the schema_migrations table if it doesn't exist
func ensureMigrationsTable(db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := db.Exec(query)
	return err
}

// GetCurrentVersion returns the current schema version
func getCurrentVersion(db *sql.DB) (int, error) {
	if err := ensureMigrationsTable(db); err != nil {
		return 0, fmt.Errorf("failed to ensure migrations table: %w", err)
	}

	var version int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("failed to get current version: %w", err)
	}

	return version, nil
}

// ApplyMigration applies a single migration
func applyMigration(db *sql.DB, migration Migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Apply the migration
	if _, err := tx.Exec(migration.Up); err != nil {
		return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
	}

	// Record the migration
	_, err = tx.Exec(
		"INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, ?)",
		migration.Version,
		migration.Description,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %d: %w", migration.Version, err)
	}

	return nil
}

// RunMigrations applies all pending migrations
func runMigrations(db *sql.DB) error {
	currentVersion, err := getCurrentVersion(db)
	if err != nil {
		return err
	}

	migrations := GetMigrations()
	applied := 0

	for _, migration := range migrations {
		if migration.Version <= currentVersion {
			continue // Already applied
		}

		if err := applyMigration(db, migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", migration.Version, err)
		}

		applied++
	}

	if applied > 0 {
		return nil // Migrations applied successfully
	}

	return nil // No migrations needed
}
