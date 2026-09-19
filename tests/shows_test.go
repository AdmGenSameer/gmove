package tests

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/samarcher/gmove/internal/config"
	"github.com/samarcher/gmove/internal/constants"
	"github.com/samarcher/gmove/internal/database"
	"github.com/samarcher/gmove/internal/deletion"
	"github.com/samarcher/gmove/internal/safety"
	"github.com/samarcher/gmove/internal/scanner"
)

func TestTVShowsScanAndRecursiveDeletion(t *testing.T) {
	tempSource := t.TempDir()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "test.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()
	repo := database.NewRepository(db)

	// Create nested TV Show structure:
	// Black Mirror/
	//   ├── Season 01/
	//   │   ├── S01E01.mkv
	//   │   └── S01E02.mkv
	//   └── Season 02/
	//       └── S02E01.mkv
	showDir := filepath.Join(tempSource, "Black Mirror")
	s1Dir := filepath.Join(showDir, "Season 01")
	s2Dir := filepath.Join(showDir, "Season 02")
	if err := os.MkdirAll(s1Dir, 0755); err != nil {
		t.Fatalf("failed to mkdir s1: %v", err)
	}
	if err := os.MkdirAll(s2Dir, 0755); err != nil {
		t.Fatalf("failed to mkdir s2: %v", err)
	}

	ep1 := filepath.Join(s1Dir, "Black.Mirror.S01E01.mkv")
	ep2 := filepath.Join(s1Dir, "Black.Mirror.S01E02.mkv")
	ep3 := filepath.Join(s2Dir, "Black.Mirror.S02E01.mkv")

	_ = os.WriteFile(ep1, []byte("ep1 content"), 0644)
	_ = os.WriteFile(ep2, []byte("ep2 content"), 0644)
	_ = os.WriteFile(ep3, []byte("ep3 content"), 0644)

	// 1. Scan TV Shows
	s := scanner.New(tempSource)
	items, err := s.Scan()
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 show item, got %d", len(items))
	}

	showItem := items[0]
	if showItem.Name != "Black Mirror" {
		t.Errorf("expected 'Black Mirror', got '%s'", showItem.Name)
	}
	if showItem.FileCount != 3 {
		t.Errorf("expected 3 files across seasons, got %d", showItem.FileCount)
	}

	// 2. Test recursive empty directory cleanup upon verified deletion
	val, _ := safety.NewValidator(tempSource)
	del := deletion.NewDeleter(repo, val, tempSource)

	opID, err := repo.CreateOperation(&database.Operation{
		StartedAt:   time.Now().UTC(),
		Status:      constants.StatusRunning,
		Source:      tempSource,
		Destination: "gdrive:Shows",
		TotalItems:  1,
		TotalFiles:  3,
	})
	if err != nil {
		t.Fatalf("create op failed: %v", err)
	}

	var dbItemIDs []int64
	for _, f := range showItem.Files {
		fi, _ := os.Stat(f.SourceAbsPath)
		var inode, dev uint64
		if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
			inode = stat.Ino
			dev = stat.Dev
		}
		item := &database.TransferItem{
			OperationID:        opID,
			Name:               f.RelativePath,
			SourceAbsPath:      f.SourceAbsPath,
			DestinationRelPath: f.RelativePath,
			SizeBytes:          fi.Size(),
			MtimeEpoch:         fi.ModTime().Unix(),
			Inode:              inode,
			DeviceID:           dev,
			Status:             constants.StatusVerified,
			VerificationStatus: constants.VerifMatched,
		}
		_ = repo.AddTransferItems([]*database.TransferItem{item})
		dbItemIDs = append(dbItemIDs, item.ID)
	}

	// Execute deletion with exact token 'DELETE'
	req := &deletion.DeleteRequest{
		OperationID:       opID,
		ItemIDs:           dbItemIDs,
		ConfirmationToken: "DELETE",
	}

	res, err := del.Execute(req)
	if err != nil {
		t.Fatalf("deletion error: %v", err)
	}
	if res.DeletedCount != 3 {
		t.Errorf("expected 3 deleted files, got %d", res.DeletedCount)
	}

	// Verify all nested season directories AND root show directory are cleanly deleted
	if _, err := os.Stat(showDir); !os.IsNotExist(err) {
		t.Errorf("expected root show directory 'Black Mirror' to be cleaned up, but it still exists")
	}
}

func TestConfigProfiles(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Source = "/mnt/media/movies"
	cfg.Remote = "gdrive"
	cfg.RemotePath = "Movies"

	cfg.Profiles["shows"] = config.Profile{
		Source:     "/mnt/nextcloud-hdd/downloads/torrents/shows",
		Remote:     "gdrive",
		RemotePath: "Shows",
	}

	// Apply shows profile
	if err := cfg.ApplyProfile("shows"); err != nil {
		t.Fatalf("apply profile failed: %v", err)
	}

	if cfg.Source != "/mnt/nextcloud-hdd/downloads/torrents/shows" {
		t.Errorf("expected shows source, got %s", cfg.Source)
	}
	if cfg.RemoteDestination() != "gdrive:Shows" {
		t.Errorf("expected 'gdrive:Shows', got '%s'", cfg.RemoteDestination())
	}
}
