package tests

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/AdmGenSameer/gmove/internal/safety"
)

func TestSafetyValidator(t *testing.T) {
	tempSource := t.TempDir()

	// 1. Root or Home directory forbidden
	if _, err := safety.NewValidator("/"); err == nil {
		t.Errorf("expected error for root directory source, got nil")
	}

	home, _ := os.UserHomeDir()
	if _, err := safety.NewValidator(home); err == nil {
		t.Errorf("expected error for user home directory source, got nil")
	}

	val, err := safety.NewValidator(tempSource)
	if err != nil {
		t.Fatalf("failed to create validator: %v", err)
	}

	// 2. Valid file path inside source
	testFile := filepath.Join(tempSource, "movie.mkv")
	if err := os.WriteFile(testFile, []byte("content 123"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	safePath, err := val.ValidatePathSafety(testFile)
	if err != nil {
		t.Errorf("expected safe path, got error: %v", err)
	}
	if safePath != testFile {
		t.Errorf("expected %s, got %s", testFile, safePath)
	}

	// 3. Path outside source rejected
	outsideFile := filepath.Join(t.TempDir(), "evil.mkv")
	if _, err := val.ValidatePathSafety(outsideFile); err == nil {
		t.Errorf("expected error for path outside source, got nil")
	}

	// 4. Path traversal (..) rejected
	traversalPath := filepath.Join(tempSource, "..", "secret.txt")
	if _, err := val.ValidatePathSafety(traversalPath); err == nil {
		t.Errorf("expected error for path traversal, got nil")
	}

	// 5. File unchanged verification
	fi, _ := os.Stat(testFile)
	var inode, dev uint64
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		inode = uint64(stat.Ino)
		dev = uint64(stat.Dev)
	}

	if err := val.ValidateFileUnchanged(testFile, fi.Size(), fi.ModTime().Unix(), inode, dev); err != nil {
		t.Errorf("expected unchanged validation to pass, got: %v", err)
	}

	// 6. Size modified on disk -> error
	if err := val.ValidateFileUnchanged(testFile, fi.Size()+10, fi.ModTime().Unix(), inode, dev); err == nil {
		t.Errorf("expected error for modified size, got nil")
	}

	// 7. Mtime modified on disk -> error
	if err := val.ValidateFileUnchanged(testFile, fi.Size(), fi.ModTime().Unix()+10, inode, dev); err == nil {
		t.Errorf("expected error for modified mtime, got nil")
	}

	// 8. Confirmation token check
	if err := val.ValidateConfirmationToken("DELETE"); err != nil {
		t.Errorf("expected 'DELETE' to pass, got: %v", err)
	}
	for _, badToken := range []string{"y", "yes", "Y", "delete", "Delete", ""} {
		if err := val.ValidateConfirmationToken(badToken); err == nil {
			t.Errorf("expected token '%s' to be rejected", badToken)
		}
	}
}
