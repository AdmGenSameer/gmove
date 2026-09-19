package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the sql.DB pool and provides migration and transaction support.
type DB struct {
	*sql.DB
}

// Open initializes the SQLite database, creates parent directories, sets WAL mode and runs schema migrations.
func Open(dbPath string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// modernc.org/sqlite DSN
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database %s: %w", dbPath, err)
	}

	// Ensure single writer concurrency safety with SQLite WAL
	db.SetMaxOpenConns(1)

	wrapped := &DB{DB: db}
	if err := wrapped.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	return wrapped, nil
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS operations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at DATETIME NOT NULL,
		completed_at DATETIME,
		status TEXT NOT NULL,
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		total_items INTEGER NOT NULL DEFAULT 0,
		total_files INTEGER NOT NULL DEFAULT 0,
		total_bytes INTEGER NOT NULL DEFAULT 0,
		dry_run BOOLEAN NOT NULL DEFAULT 0,
		notes TEXT
	);

	CREATE TABLE IF NOT EXISTS transfer_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		operation_id INTEGER NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		is_directory BOOLEAN NOT NULL DEFAULT 0,
		relative_path TEXT NOT NULL,
		source_abs_path TEXT NOT NULL,
		destination_rel_path TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		mtime_epoch INTEGER NOT NULL,
		inode INTEGER NOT NULL DEFAULT 0,
		device_id INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL,
		verification_status TEXT NOT NULL,
		source_hash TEXT,
		remote_hash TEXT,
		error TEXT,
		transferred_at DATETIME,
		verified_at DATETIME,
		deleted_at DATETIME
	);

	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		operation_id INTEGER REFERENCES operations(id) ON DELETE CASCADE,
		timestamp DATETIME NOT NULL,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		details TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_transfer_items_op_status ON transfer_items(operation_id, status);
	CREATE INDEX IF NOT EXISTS idx_transfer_items_rel_path ON transfer_items(relative_path);
	CREATE INDEX IF NOT EXISTS idx_operations_status ON operations(status);
	`

	_, err := db.Exec(schema)
	return err
}
