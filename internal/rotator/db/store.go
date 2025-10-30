package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "embed"

	_ "github.com/mattn/go-sqlite3"
)

// Store defines all functions to interact with the database
type Store interface {
	Querier
	// Transaction support
	BeginTx(ctx context.Context) (*sql.Tx, error)
	ExecTx(ctx context.Context, fn func(*Queries) error) error
	// Health check
	Ping(ctx context.Context) error
	// Cleanup
	Close() error
}

// SQLStore provides all functions to execute db queries and transactions
type SQLStore struct {
	*Queries
	db *sql.DB
}

// Config holds database configuration
type Config struct {
	Path            string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime int // seconds
}

// DefaultConfig returns default database configuration
func DefaultConfig() *Config {
	return &Config{
		Path:            "./data/rotator.db",
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 300, // 5 minutes
	}
}

//go:embed schema.sql
var ddl string

// NewStore creates a new Store with automatic schema setup and migrations
func NewStore(config *Config) (Store, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Ensure directory exists
	dir := filepath.Dir(config.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Open database with proper settings
	db, err := sql.Open("sqlite3", config.Path+"?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	if config.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(getDuration(config.ConnMaxLifetime))
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Create store instance
	store := &SQLStore{
		Queries: New(db),
		db:      db,
	}

	// Initialize schema
	if err := store.SetupSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to setup database schema: %w", err)
	}

	return store, nil
}

// NewStoreFromDB creates a new Store from an existing database connection
// Useful for testing
func NewStoreFromDB(db *sql.DB) Store {
	return &SQLStore{
		Queries: New(db),
		db:      db,
	}
}

// SetupSchema sets up the database schema
func (s *SQLStore) SetupSchema() error {
	// If key table already exists, assume schema is present
	var name string
	if err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name = ?;", "peers").Scan(&name); err == nil {
		return nil
	} else if err != sql.ErrNoRows {
		return fmt.Errorf("failed to check existing schema: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin schema transaction: %w", err)
	}

	if _, err := tx.Exec(ddl); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to setup database schema: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit schema transaction: %w", err)
	}

	return nil
}

// BeginTx starts a new transaction
func (s *SQLStore) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// ExecTx executes a function within a transaction
func (s *SQLStore) ExecTx(ctx context.Context, fn func(*Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	q := New(tx)
	err = fn(q)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction error: %v, rollback error: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Ping checks if the database connection is alive
func (s *SQLStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the database connection
func (s *SQLStore) Close() error {
	return s.db.Close()
}

// GetDB returns the underlying database connection (useful for advanced operations)
func (s *SQLStore) GetDB() *sql.DB {
	return s.db
}

// Helper function to get duration from seconds
func getDuration(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}
