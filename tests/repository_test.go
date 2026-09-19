package tests

import (
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
