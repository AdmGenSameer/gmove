package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/logger"
)

var (
	ErrUnsafeSourceDir      = errors.New("source directory is unsafe (e.g. root or home dir)")
	ErrPathOutsideSource    = errors.New("target path is outside configured source directory")
	ErrFileModifiedOnDisk   = errors.New("local file was modified on disk after discovery")
	ErrInvalidDeleteToken   = errors.New("invalid confirmation token (must be exact uppercase 'DELETE')")
	ErrSymlinkEscape        = errors.New("symlink escapes source directory")
	ErrFileNotFound         = errors.New("target file does not exist on disk")
)

// Validator enforces filesystem safety and identity verification.
type Validator struct {
	sourceDir string
}

func NewValidator(sourceDir string) (*Validator, error) {
	cleanSource := filepath.Clean(sourceDir)
	if !filepath.IsAbs(cleanSource) {
		err := fmt.Errorf("source directory must be an absolute path: %s", sourceDir)
		logger.Errorf("safety", "%v", err)
		return nil, err
	}

	// Reject root directory
	if cleanSource == "/" {
		err := fmt.Errorf("%w: root directory '/' is forbidden as source", ErrUnsafeSourceDir)
		logger.Errorf("safety", "%v", err)
		return nil, err
	}

	// Reject user home directory root (e.g. /home/username)
	home, err := os.UserHomeDir()
	if err == nil && cleanSource == filepath.Clean(home) {
		err := fmt.Errorf("%w: user home directory '%s' is forbidden as source", ErrUnsafeSourceDir, home)
		logger.Errorf("safety", "%v", err)
		return nil, err
	}

	logger.Infof("safety", "Safety validator active for source: %s", cleanSource)
	return &Validator{sourceDir: cleanSource}, nil
}

// ValidatePathSafety verifies that targetPath is strictly inside the configured source directory.
func (v *Validator) ValidatePathSafety(targetPath string) (string, error) {
	cleanTarget := filepath.Clean(targetPath)
	if !filepath.IsAbs(cleanTarget) {
		cleanTarget = filepath.Clean(filepath.Join(v.sourceDir, targetPath))
	}

	rel, err := filepath.Rel(v.sourceDir, cleanTarget)
	if err != nil {
		logger.Errorf("safety", "Path calculation failed for %s: %v", cleanTarget, err)
		return "", fmt.Errorf("%w: %v", ErrPathOutsideSource, err)
	}

	if rel == "." || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)) {
		logger.Warnf("safety", "Path escape rejected: '%s' is not strictly inside '%s'", cleanTarget, v.sourceDir)
		return "", fmt.Errorf("%w: path '%s' is not strictly within '%s'", ErrPathOutsideSource, cleanTarget, v.sourceDir)
	}

	// Verify symlink target if path is or traverses a symlink
	realTarget, err := filepath.EvalSymlinks(cleanTarget)
	if err == nil {
		realRel, err := filepath.Rel(v.sourceDir, realTarget)
		if err != nil || realRel == "." || strings.HasPrefix(realRel, "..") {
			logger.Errorf("safety", "Symlink escape detected: '%s' resolves outside source to '%s'", cleanTarget, realTarget)
			return "", fmt.Errorf("%w: symlink target '%s' escapes source root '%s'", ErrSymlinkEscape, realTarget, v.sourceDir)
		}
	}

	return cleanTarget, nil
}

// ValidateFileUnchanged checks that the file on disk still matches the recorded size, mtime, and inode.
func (v *Validator) ValidateFileUnchanged(targetPath string, expectedSize int64, expectedMtime int64, expectedInode uint64, expectedDev uint64) error {
	fi, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Errorf("safety", "Target file vanished before deletion: %s", targetPath)
			return fmt.Errorf("%w: %s", ErrFileNotFound, targetPath)
		}
		logger.Errorf("safety", "Failed to stat file %s: %v", targetPath, err)
		return fmt.Errorf("failed to stat file %s: %w", targetPath, err)
	}

	// 1. Check size
	if fi.Size() != expectedSize {
		logger.Errorf("safety", "File size drifted on disk: %s (expected %d, got %d)", targetPath, expectedSize, fi.Size())
		return fmt.Errorf("%w: size changed from %d to %d bytes for %s", ErrFileModifiedOnDisk, expectedSize, fi.Size(), targetPath)
	}

	// 2. Check mtime
	if fi.ModTime().Unix() != expectedMtime {
		logger.Errorf("safety", "File mtime drifted on disk: %s (expected %d, got %d)", targetPath, expectedMtime, fi.ModTime().Unix())
		return fmt.Errorf("%w: mtime changed from %d to %d for %s", ErrFileModifiedOnDisk, expectedMtime, fi.ModTime().Unix(), targetPath)
	}

	// 3. Check inode / dev ID (on Linux and Unix systems)
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if expectedInode > 0 && uint64(stat.Ino) != expectedInode {
			logger.Errorf("safety", "File inode changed: %s (expected %d, got %d)", targetPath, expectedInode, stat.Ino)
			return fmt.Errorf("%w: inode changed from %d to %d for %s", ErrFileModifiedOnDisk, expectedInode, stat.Ino, targetPath)
		}
		if expectedDev > 0 && uint64(stat.Dev) != expectedDev {
			logger.Errorf("safety", "File device ID changed: %s (expected %d, got %d)", targetPath, expectedDev, stat.Dev)
			return fmt.Errorf("%w: device ID changed from %d to %d for %s", ErrFileModifiedOnDisk, expectedDev, stat.Dev, targetPath)
		}
	}

	return nil
}

// ValidateConfirmationToken checks for exact token "DELETE".
func (v *Validator) ValidateConfirmationToken(token string) error {
	trimmed := strings.TrimSpace(token)
	if trimmed != constants.RequiredDeleteToken {
		logger.Warnf("safety", "Rejected deletion confirmation token: %q", token)
		return fmt.Errorf("%w: received '%s'", ErrInvalidDeleteToken, trimmed)
	}
	logger.Infof("safety", "Accepted explicit confirmation token 'DELETE'")
	return nil
}
