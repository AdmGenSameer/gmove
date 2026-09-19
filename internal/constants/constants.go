package constants

import (
	"os"
	"path/filepath"
)

const (
	Version = "1.0.4"
)

// Item and Operation Statuses
type Status string

const (
	StatusDiscovered            Status = "DISCOVERED"
	StatusSelected              Status = "SELECTED"
	StatusPending               Status = "PENDING"
	StatusTransferring          Status = "TRANSFERRING"
	StatusTransferred           Status = "TRANSFERRED"
	StatusVerifying             Status = "VERIFYING"
	StatusVerified              Status = "VERIFIED"
	StatusDeletePending         Status = "DELETE_PENDING"
	StatusDeleted               Status = "DELETED"
	StatusFailed                Status = "FAILED"
	StatusSkipped               Status = "SKIPPED"
	StatusInterrupted           Status = "INTERRUPTED"
	StatusRunning               Status = "RUNNING"
	StatusCompleted             Status = "COMPLETED"
	StatusCompletedWithErrors   Status = "COMPLETED_WITH_ERRORS"
)

// Verification Statuses
type VerificationStatus string

const (
	VerifUnverified       VerificationStatus = "UNVERIFIED"
	VerifMatched          VerificationStatus = "MATCHED"
	VerifMismatchSize     VerificationStatus = "MISMATCH_SIZE"
	VerifMismatchHash     VerificationStatus = "MISMATCH_HASH"
	VerifMissingOnRemote  VerificationStatus = "MISSING_ON_REMOTE"
	VerifError            VerificationStatus = "ERROR"
)

// Confirmation token required for deletion
const RequiredDeleteToken = "DELETE"

// Supported Media File Extensions
var MediaExtensions = map[string]bool{
	".mkv":  true,
	".mp4":  true,
	".avi":  true,
	".mov":  true,
	".webm": true,
	".m4v":  true,
	".ts":   true,
	".m2ts": true,
}

// Associated Sidecar Extensions (preserved when migrating directory bundles)
var SidecarExtensions = map[string]bool{
	".nfo":  true,
	".srt":  true,
	".sub":  true,
	".idx":  true,
	".vtt":  true,
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".xml":  true,
	".txt":  true,
}

// Incomplete or temporary download extensions to exclude
var IncompleteExtensions = map[string]bool{
	".part":        true,
	".parts":       true,
	".!qb":         true,
	".crdownload":  true,
	".tmp":         true,
	".downloading": true,
	".torrent":     true,
}

// Default directory names to ignore (system folders, non-media vaults, app directories)
var DefaultIgnoreDirs = []string{
	"Cloudbackup",
	"Music",
	"incomplete",
	"prowlarr",
	"sonarr",
	"radarr",
	"lost+found",
	"@eaDir",
	".recycle",
	"#recycle",
}

// Default Paths
func DefaultConfigPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, _ := os.UserHomeDir()
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "gmove", "config.toml")
}

func DefaultDatabasePath() string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "gmove", "gmove.db")
}

func DefaultLogPath() string {
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, _ := os.UserHomeDir()
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "gmove", "gmove.log")
}
