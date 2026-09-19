package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samarcher/gmove/internal/scanner"
)

func TestScanner(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Single movie file
	singleMovie := filepath.Join(tempDir, "Interstellar (2014).mkv")
	if err := os.WriteFile(singleMovie, []byte("dummy video content 1"), 0644); err != nil {
		t.Fatalf("failed to create single movie: %v", err)
	}

	// 2. Movie folder with video + sidecars
	movieDir := filepath.Join(tempDir, "Dune (2021)")
	if err := os.MkdirAll(movieDir, 0755); err != nil {
		t.Fatalf("failed to create movie dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "Dune (2021).mp4"), []byte("dune video"), 0644); err != nil {
		t.Fatalf("failed to write movie file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "Dune.nfo"), []byte("<movie>Dune</movie>"), 0644); err != nil {
		t.Fatalf("failed to write nfo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "poster.jpg"), []byte("jpg data"), 0644); err != nil {
		t.Fatalf("failed to write poster: %v", err)
	}
	if err := os.WriteFile(filepath.Join(movieDir, "english.srt"), []byte("1\n00:00:01 --> 00:00:04\nHello"), 0644); err != nil {
		t.Fatalf("failed to write srt: %v", err)
	}

	// 3. Incomplete download that should be ignored
	incompleteFile := filepath.Join(tempDir, "DownloadingMovie.mkv.part")
	if err := os.WriteFile(incompleteFile, []byte("incomplete"), 0644); err != nil {
		t.Fatalf("failed to write part file: %v", err)
	}

	// 4. Directory without any video file (e.g. non-media folder)
	otherDir := filepath.Join(tempDir, "Documents")
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatalf("failed to create other dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "notes.txt"), []byte("notes"), 0644); err != nil {
		t.Fatalf("failed to write notes: %v", err)
	}

	// 5. Cloudbackup vault folder that must be ignored
	cloudBackupDir := filepath.Join(tempDir, "Cloudbackup")
	if err := os.MkdirAll(cloudBackupDir, 0755); err != nil {
		t.Fatalf("failed to create Cloudbackup dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cloudBackupDir, "vault.cryptomator"), []byte("vault"), 0644); err != nil {
		t.Fatalf("failed to write vault file: %v", err)
	}

	// 6. Music folder that must be ignored even if it contains a media file
	musicDir := filepath.Join(tempDir, "Music")
	if err := os.MkdirAll(musicDir, 0755); err != nil {
		t.Fatalf("failed to create Music dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(musicDir, "music_video.mp4"), []byte("video"), 0644); err != nil {
		t.Fatalf("failed to write music video file: %v", err)
	}

	// 7. Prowlarr app folder that must be ignored
	prowlarrDir := filepath.Join(tempDir, "prowlarr")
	if err := os.MkdirAll(prowlarrDir, 0755); err != nil {
		t.Fatalf("failed to create prowlarr dir: %v", err)
	}

	// Scan
	s := scanner.New(tempDir)
	items, err := s.Scan()
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	// Expect exactly 2 items: "Dune (2021)" and "Interstellar (2014).mkv"
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	var duneItem, interstellarItem *scanner.MediaItem
	for _, it := range items {
		if it.Name == "Dune (2021)" {
			duneItem = it
		} else if it.Name == "Interstellar (2014).mkv" {
			interstellarItem = it
		}
	}

	if duneItem == nil || !duneItem.IsDirectory {
		t.Fatalf("expected Dune (2021) directory bundle")
	}
	if duneItem.FileCount != 4 {
		t.Errorf("expected 4 files in Dune bundle, got %d", duneItem.FileCount)
	}
	if duneItem.Inode == 0 {
		t.Errorf("expected non-zero inode for Dune directory")
	}

	if interstellarItem == nil || interstellarItem.IsDirectory {
		t.Fatalf("expected Interstellar single file")
	}
	if interstellarItem.FileCount != 1 {
		t.Errorf("expected 1 file for Interstellar, got %d", interstellarItem.FileCount)
	}

	// Test filtering
	filtered := scanner.Filter(items, "dune")
	if len(filtered) != 1 || filtered[0].Name != "Dune (2021)" {
		t.Errorf("filter failed, expected 1 Dune item, got %d", len(filtered))
	}

	// Test sorting
	scanner.Sort(items, scanner.SortBySize, true)
	if items[0].SizeBytes < items[1].SizeBytes {
		t.Errorf("descending sort by size failed")
	}
}
