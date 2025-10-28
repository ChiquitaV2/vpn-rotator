package db

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// NewTestDB creates a new in-memory SQLite database for testing
func NewTestDB(t *testing.T) (*sql.DB, Store) {
	t.Helper()

	// Use in-memory database with shared cache mode
	// This ensures all connections see the same database
	db, err := sql.Open("sqlite3", "file::memory:?mode=memory&cache=shared&_foreign_keys=on")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	// Set connection pool to 1 for consistent testing
	db.SetMaxOpenConns(1)

	// Initialize schema using the new store
	store := NewStoreFromDB(db)
	if err := store.(*SQLStore).Setup(context.Background()); err != nil {
		db.Close()
		t.Fatalf("failed to setup test database schema: %v", err)
	}

	// Register cleanup
	t.Cleanup(func() {
		db.Close()
	})

	return db, store
}

// TruncateTables removes all data from tables while preserving schema
func TruncateTables(t *testing.T, db *sql.DB) {
	t.Helper()

	tables := []string{"nodes"}
	for _, table := range tables {
		_, err := db.Exec("DELETE FROM " + table)
		if err != nil {
			t.Fatalf("failed to truncate table %s: %v", table, err)
		}
	}
}

// SeedTestNode creates a test node in the database
func SeedTestNode(t *testing.T, store Store, params CreateNodeParams) Node {
	t.Helper()

	node, err := store.CreateNode(context.Background(), params)
	if err != nil {
		t.Fatalf("failed to seed test node: %v", err)
	}

	return node
}
