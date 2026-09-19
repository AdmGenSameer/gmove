package tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samarcher/gmove/internal/config"
	"github.com/samarcher/gmove/internal/constants"
	"github.com/samarcher/gmove/internal/database"
	"github.com/samarcher/gmove/internal/rclone"
	"github.com/samarcher/gmove/internal/transfer"
)

func TestResumeAndRetry(t *testing.T) {
	tempSource := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	mockClient := rclone.NewMockClient()

	cfg := &config.Config{
		Source:     tempSource,
		Remote:     "gdrive",
		RemotePath: "Movies",
	}

	mgr := transfer.NewManager(cfg, repo, mockClient)

	// Create an interrupted operation with 1 verified item, 1 failed item, 1 pending item
	opID, err := repo.CreateOperation(&database.Operation{
		StartedAt:   time.Now().UTC(),
		Status:      constants.StatusInterrupted,
		Source:      tempSource,
		Destination: "gdrive:Movies",
		TotalItems:  3,
		TotalFiles:  3,
		TotalBytes:  3000,
	})
	if err != nil {
		t.Fatalf("failed to create operation: %v", err)
	}

	items := []*database.TransferItem{
		{
			OperationID:        opID,
			Name:               "item1.mkv",
			SourceAbsPath:      filepath.Join(tempSource, "item1.mkv"),
			DestinationRelPath: "item1.mkv",
			SizeBytes:          1000,
			Status:             constants.StatusVerified, // Already done!
			VerificationStatus: constants.VerifMatched,
		},
		{
			OperationID:        opID,
			Name:               "item2.mkv",
			SourceAbsPath:      filepath.Join(tempSource, "item2.mkv"),
			DestinationRelPath: "item2.mkv",
			SizeBytes:          1000,
			Status:             constants.StatusFailed, // Failed
			VerificationStatus: constants.VerifError,
		},
		{
			OperationID:        opID,
			Name:               "item3.mkv",
			SourceAbsPath:      filepath.Join(tempSource, "item3.mkv"),
			DestinationRelPath: "item3.mkv",
			SizeBytes:          1000,
			Status:             constants.StatusPending, // Not yet started
			VerificationStatus: constants.VerifUnverified,
		},
	}
	if err := repo.AddTransferItems(items); err != nil {
		t.Fatalf("failed to add items: %v", err)
	}

	// 1. Test Incomplete Operation Detection
	inc, err := repo.GetIncompleteOperation()
	if err != nil || inc == nil {
		t.Fatalf("expected incomplete operation, got %v, err %v", inc, err)
	}
	if inc.ID != opID {
		t.Errorf("expected opID %d, got %d", opID, inc.ID)
	}

	// 2. Test Resume
	err = mgr.Execute(context.Background(), opID, nil)
	if err != nil {
		t.Fatalf("resume execution failed: %v", err)
	}

	// All items should now be VERIFIED
	verified, err := repo.GetVerifiedItems(opID)
	if err != nil {
		t.Fatalf("failed to get verified: %v", err)
	}
	if len(verified) != 3 {
		t.Errorf("expected 3 verified items after resume, got %d", len(verified))
	}

	// 3. Test Retry Logic: Simulate 1 item failure
	failedItem := verified[1]
	_ = repo.UpdateItemStatus(failedItem.ID, constants.StatusFailed, constants.VerifError, "simulated network timeout")
	_ = repo.UpdateOperationStatus(opID, constants.StatusCompletedWithErrors, nil)

	failedItems, err := repo.GetFailedItems(opID)
	if err != nil || len(failedItems) != 1 {
		t.Fatalf("expected 1 failed item, got %d", len(failedItems))
	}

	// Run retry
	err = mgr.RetryFailed(context.Background(), opID, nil)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}

	finalFailed, _ := repo.GetFailedItems(opID)
	if len(finalFailed) != 0 {
		t.Errorf("expected 0 failed items after retry, got %d", len(finalFailed))
	}
}
