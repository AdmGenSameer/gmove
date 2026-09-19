package tests

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/deletion"
	"github.com/AdmGenSameer/gmove/internal/safety"
)

func TestDeleter(t *testing.T) {
	tempSource := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	val, err := safety.NewValidator(tempSource)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}
	del := deletion.NewDeleter(repo, val, tempSource)

	// Create operation
	opID, err := repo.CreateOperation(&database.Operation{
		StartedAt:   time.Now().UTC(),
		Status:      constants.StatusRunning,
		Source:      tempSource,
		Destination: "gdrive:Movies",
		TotalItems:  2,
		TotalFiles:  2,
	})
	if err != nil {
		t.Fatalf("failed to create op: %v", err)
	}

	// Create real file on disk
	filePath1 := filepath.Join(tempSource, "verified_movie.mkv")
	content := []byte("dummy video verified")
	if err := os.WriteFile(filePath1, content, 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	fi, _ := os.Stat(filePath1)
	var inode, dev uint64
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		inode = stat.Ino
		dev = stat.Dev
	}

	// Add verified item to DB
	items := []*database.TransferItem{
		{
			OperationID:        opID,
			Name:               "verified_movie.mkv",
			SourceAbsPath:      filePath1,
			DestinationRelPath: "verified_movie.mkv",
			SizeBytes:          fi.Size(),
			MtimeEpoch:         fi.ModTime().Unix(),
			Inode:              inode,
			DeviceID:           dev,
			Status:             constants.StatusVerified,
			VerificationStatus: constants.VerifMatched,
		},
		{
			OperationID:        opID,
			Name:               "unverified_movie.mkv",
			SourceAbsPath:      filepath.Join(tempSource, "unverified_movie.mkv"),
			DestinationRelPath: "unverified_movie.mkv",
			SizeBytes:          100,
			MtimeEpoch:         time.Now().Unix(),
			Status:             constants.StatusTransferred, // NOT VERIFIED!
			VerificationStatus: constants.VerifUnverified,
		},
	}
	if err := repo.AddTransferItems(items); err != nil {
		t.Fatalf("failed to add items: %v", err)
	}

	// 1. Rejection if token is not DELETE
	badReq := &deletion.DeleteRequest{
		OperationID:       opID,
		ItemIDs:           []int64{items[0].ID},
		ConfirmationToken: "yes",
	}
	if _, err := del.Execute(badReq); err == nil {
		t.Errorf("expected rejection for token 'yes', got nil")
	}

	// 2. Rejection if status is not VERIFIED
	unverifiedReq := &deletion.DeleteRequest{
		OperationID:       opID,
		ItemIDs:           []int64{items[1].ID},
		ConfirmationToken: "DELETE",
	}
	res, err := del.Execute(unverifiedReq)
	if err != nil {
		t.Fatalf("unexpected execution error: %v", err)
	}
	if len(res.Failed) != 1 || res.DeletedCount != 0 {
		t.Errorf("expected failure for unverified item, got deleted count %d", res.DeletedCount)
	}

	// 3. Successful deletion of verified item with exact token 'DELETE'
	validReq := &deletion.DeleteRequest{
		OperationID:       opID,
		ItemIDs:           []int64{items[0].ID},
		ConfirmationToken: "DELETE",
	}
	res, err = del.Execute(validReq)
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}
	if res.DeletedCount != 1 {
		t.Errorf("expected 1 deleted item, got %d", res.DeletedCount)
	}
	if _, err := os.Stat(filePath1); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted from disk, but it still exists")
	}

	// Verify DB status is now DELETED
	updatedItem, err := repo.GetTransferItem(items[0].ID)
	if err != nil {
		t.Fatalf("failed to fetch item: %v", err)
	}
	if updatedItem.Status != constants.StatusDeleted {
		t.Errorf("expected status DELETED, got %s", updatedItem.Status)
	}
	if updatedItem.DeletedAt == nil {
		t.Errorf("expected non-nil deleted_at timestamp")
	}
}
