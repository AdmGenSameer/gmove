package tests

import (
	"path/filepath"
	"testing"

	"github.com/samarcher/gmove/internal/config"
)

func TestConfigValidationAndSave(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	cfg := config.DefaultConfig()
	cfg.Source = filepath.Join(tempDir, "Movies")
	cfg.Remote = "gdrive"
	cfg.RemotePath = "Movies"
	cfg.Database = filepath.Join(tempDir, "gmove.db")

	if err := cfg.Validate(); err != nil {
		t.Fatalf("validation failed: %v", err)
	}

	if err := cfg.Save(configPath); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, _, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if loaded.Source != cfg.Source {
		t.Errorf("expected source %s, got %s", cfg.Source, loaded.Source)
	}
	if loaded.Remote != cfg.Remote {
		t.Errorf("expected remote %s, got %s", cfg.Remote, loaded.Remote)
	}
	if loaded.RemoteDestination() != "gdrive:Movies" {
		t.Errorf("expected destination 'gdrive:Movies', got '%s'", loaded.RemoteDestination())
	}
}

func TestConfigValidationErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	// Missing source
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected error for empty source, got nil")
	}

	// Relative source path
	cfg.Source = "relative/path"
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected error for relative source path, got nil")
	}

	// Absolute source but missing remote
	cfg.Source = "/mnt/hdd/Movies"
	cfg.Remote = ""
	if err := cfg.Validate(); err == nil {
		t.Errorf("expected error for empty remote, got nil")
	}
}
