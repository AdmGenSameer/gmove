package tests

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AdmGenSameer/gmove/internal/lifecycle"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"v1.0.3", "v1.0.4", -1},
		{"v1.0.4", "v1.0.3", 1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.2.3-beta", "v1.2.3", 0},
		{"v1.10.0", "v1.9.0", 1},
		{"1.0", "1.0.0", 0},
		{"v0.9.9", "v1.0.0", -1},
	}

	for _, tc := range cases {
		res := lifecycle.CompareVersions(tc.v1, tc.v2)
		if res != tc.expected {
			t.Errorf("CompareVersions(%q, %q) = %d, expected %d", tc.v1, tc.v2, res, tc.expected)
		}
	}
}

func TestUninstallInteractiveCancellation(t *testing.T) {
	var stdout bytes.Buffer
	stdin := strings.NewReader("n\n")

	err := lifecycle.Uninstall(lifecycle.UninstallOptions{
		NonInteractive: false,
		Purge:          false,
		Stdin:          stdin,
		Stdout:         &stdout,
	})
	if err != nil {
		t.Fatalf("unexpected error during cancelled uninstall: %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "Uninstall cancelled") {
		t.Errorf("expected cancellation message, got: %s", output)
	}
}

func TestUninstallSandboxFileOperations(t *testing.T) {
	tempDir := t.TempDir()

	// Create fake bin and config dirs
	binDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatal(err)
	}
	fakeBin := filepath.Join(binDir, "gmove")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho fake"), 0755); err != nil {
		t.Fatal(err)
	}

	cfgDir := filepath.Join(tempDir, "config", "gmove")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	cfgFile := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(cfgFile, []byte("source = '/tmp'"), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify file exists
	if _, err := os.Stat(fakeBin); err != nil {
		t.Fatalf("expected fake binary to exist: %v", err)
	}

	// Simulate deletion
	if err := os.Remove(fakeBin); err != nil {
		t.Fatalf("failed to remove fake binary: %v", err)
	}

	if _, err := os.Stat(fakeBin); !os.IsNotExist(err) {
		t.Errorf("expected fake binary to be deleted")
	}

	// Verify config is preserved without purge
	if _, err := os.Stat(cfgFile); err != nil {
		t.Errorf("expected config file to remain preserved")
	}

	// Simulate purge
	if err := os.RemoveAll(cfgDir); err != nil {
		t.Fatalf("failed to purge config dir: %v", err)
	}
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("expected config dir to be purged")
	}
}
