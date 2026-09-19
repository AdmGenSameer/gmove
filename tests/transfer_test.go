package tests

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdmGenSameer/gmove/internal/config"
	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/rclone"
	"github.com/AdmGenSameer/gmove/internal/scanner"
	"github.com/AdmGenSameer/gmove/internal/transfer"
)

func TestTransferManager(t *testing.T) {
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
		Transfers:  2,
		Checkers:   4,
	}

	mgr := transfer.NewManager(cfg, repo, mockClient)

	// 1. Preflight check
	if err := mgr.PreflightCheck(context.Background()); err != nil {
		t.Fatalf("preflight check failed: %v", err)
	}

	// 2. Prepare operation
	items := []*scanner.MediaItem{
		{
			Name:          "Oppenheimer (2023).mkv",
			IsDirectory:   false,
			RelativePath:  "Oppenheimer (2023).mkv",
			SourceAbsPath: filepath.Join(tempSource, "Oppenheimer (2023).mkv"),
			SizeBytes:     21 * 1024 * 1024 * 1024,
			FileCount:     1,
			MtimeEpoch:    time.Now().Unix(),
		},
		{
			Name:          "Avatar (2022).mkv",
			IsDirectory:   false,
			RelativePath:  "Avatar (2022).mkv",
			SourceAbsPath: filepath.Join(tempSource, "Avatar (2022).mkv"),
			SizeBytes:     31 * 1024 * 1024 * 1024,
			FileCount:     1,
			MtimeEpoch:    time.Now().Unix(),
		},
	}

	op, dbItems, err := mgr.PrepareOperation(items, false)
	if err != nil {
		t.Fatalf("prepare operation failed: %v", err)
	}
	if len(dbItems) != 2 {
		t.Fatalf("expected 2 db items, got %d", len(dbItems))
	}

	// 3. Execute with progress events
	var events []transfer.TransferEvent
	err = mgr.Execute(context.Background(), op.ID, func(ev transfer.TransferEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	// Check that both items reached VERIFIED
	verifiedItems, err := repo.GetVerifiedItems(op.ID)
	if err != nil {
		t.Fatalf("failed to query verified items: %v", err)
	}
	if len(verifiedItems) != 2 {
		t.Errorf("expected 2 verified items, got %d", len(verifiedItems))
	}

	// Check operation completed status
	updatedOp, err := repo.GetOperation(op.ID)
	if err != nil {
		t.Fatalf("failed to query op: %v", err)
	}
	if updatedOp.Status != constants.StatusCompleted {
		t.Errorf("expected op status COMPLETED, got %s", updatedOp.Status)
	}
}
