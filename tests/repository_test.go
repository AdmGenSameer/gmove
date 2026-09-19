package tests

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
)

func TestDatabaseRepository(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)

	// Create operation
	op := &database.Operation{
		StartedAt:   time.Now().UTC(),
		Status:      constants.StatusRunning,
		Source:      "/mnt/hdd/Movies",
		Destination: "gdrive:Movies",
		TotalItems:  2,
		TotalFiles:  2,
		TotalBytes:  50 * 1024 * 1024 * 1024,
	}

	opID, err := repo.CreateOperation(op)
	if err != nil {
		t.Fatalf("failed to create operation: %v", err)
	}
	if opID <= 0 {
		t.Fatalf("expected positive opID, got %d", opID)
	}

	// Add transfer items
	items := []*database.TransferItem{
		{
			OperationID:        opID,
			Name:               "Interstellar (2014).mkv",
			IsDirectory:        false,
			RelativePath:       "Interstellar (2014).mkv",
			SourceAbsPath:      "/mnt/hdd/Movies/Interstellar (2014).mkv",
			DestinationRelPath: "Interstellar (2014).mkv",
			SizeBytes:          27 * 1024 * 1024 * 1024,
			MtimeEpoch:         1700000000,
			Inode:              123456,
			DeviceID:           65,
			Status:             constants.StatusPending,
			VerificationStatus: constants.VerifUnverified,
		},
		{
			OperationID:        opID,
			Name:               "Dune (2021).mkv",
			IsDirectory:        false,
			RelativePath:       "Dune (2021).mkv",
			SourceAbsPath:      "/mnt/hdd/Movies/Dune (2021).mkv",
			DestinationRelPath: "Dune (2021).mkv",
			SizeBytes:          18 * 1024 * 1024 * 1024,
			MtimeEpoch:         1700000100,
			Inode:              123457,
			DeviceID:           65,
			Status:             constants.StatusPending,
			VerificationStatus: constants.VerifUnverified,
		},
	}

	if err := repo.AddTransferItems(items); err != nil {
		t.Fatalf("failed to add transfer items: %v", err)
	}

	fetchedItems, err := repo.GetTransferItems(opID)
	if err != nil {
		t.Fatalf("failed to get items: %v", err)
	}
	if len(fetchedItems) != 2 {
		t.Fatalf("expected 2 items, got %d", len(fetchedItems))
	}

	// Test status transition
	item1 := fetchedItems[0]
	now := time.Now().UTC()
	if err := repo.UpdateItemTransferred(item1.ID, now); err != nil {
		t.Fatalf("failed to update transferred: %v", err)
	}

	if err := repo.UpdateItemVerified(item1.ID, now, "hash-src-123", "hash-remote-123"); err != nil {
		t.Fatalf("failed to update verified: %v", err)
	}

	verifiedItems, err := repo.GetVerifiedItems(opID)
	if err != nil {
		t.Fatalf("failed to get verified items: %v", err)
	}
	if len(verifiedItems) != 1 {
		t.Fatalf("expected 1 verified item, got %d", len(verifiedItems))
	}
	if verifiedItems[0].SourceHash != "hash-src-123" {
		t.Errorf("expected source hash 'hash-src-123', got '%s'", verifiedItems[0].SourceHash)
	}
}

func TestLegacyDatabaseMigration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "legacy.db")

	// 1. Create a database using the legacy v1.0.3 schema where events table did not have 'component' column
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	legacySchema := `
	CREATE TABLE operations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at DATETIME NOT NULL,
		completed_at DATETIME,
		status TEXT NOT NULL,
		source TEXT NOT NULL,
		destination TEXT NOT NULL,
		total_items INTEGER NOT NULL,
		total_files INTEGER NOT NULL,
		total_bytes INTEGER NOT NULL DEFAULT 0,
		transferred_bytes INTEGER NOT NULL DEFAULT 0,
		error TEXT
	);
	CREATE TABLE transfer_items (
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
	CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		operation_id INTEGER REFERENCES operations(id) ON DELETE CASCADE,
		timestamp DATETIME NOT NULL,
		level TEXT NOT NULL,
		message TEXT NOT NULL,
		details TEXT
	);
	`
	if _, err := rawDB.Exec(legacySchema); err != nil {
		t.Fatal(err)
	}
	rawDB.Close()

	// 2. Open with database.Open, which runs migrate()
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to migrate legacy database: %v", err)
	}
	defer db.Close()

	// 3. Verify that component column exists and can be written and queried
	repo := database.NewRepository(db)
	if err := repo.LogEvent(nil, "INFO", "test_comp", "migration success", ""); err != nil {
		t.Fatalf("failed to log event to migrated table: %v", err)
	}

	records, err := repo.QueryEvents(database.EventFilter{Component: "test_comp"})
	if err != nil {
		t.Fatalf("failed to query migrated table: %v", err)
	}
	if len(records) != 1 || records[0].Component != "test_comp" {
		t.Fatalf("expected 1 record with component 'test_comp', got %v", records)
	}
}
