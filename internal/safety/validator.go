package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/AdmGenSameer/gmove/internal/constants"
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
		return nil, fmt.Errorf("source directory must be an absolute path: %s", sourceDir)
	}

	// Reject root directory
	if cleanSource == "/" {
		return nil, fmt.Errorf("%w: root directory '/' is forbidden as source", ErrUnsafeSourceDir)
	}

	// Reject user home directory root (e.g. /home/username)
	home, err := os.UserHomeDir()
	if err == nil && cleanSource == filepath.Clean(home) {
		return nil, fmt.Errorf("%w: user home directory '%s' is forbidden as source", ErrUnsafeSourceDir, home)
	}

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
		return "", fmt.Errorf("%w: %v", ErrPathOutsideSource, err)
	}

	if rel == "." || strings.HasPrefix(rel, "..") || strings.HasPrefix(rel, string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path '%s' is not strictly within '%s'", ErrPathOutsideSource, cleanTarget, v.sourceDir)
	}

	// Verify symlink target if path is or traverses a symlink
	realTarget, err := filepath.EvalSymlinks(cleanTarget)
	if err == nil {
		realRel, err := filepath.Rel(v.sourceDir, realTarget)
		if err != nil || realRel == "." || strings.HasPrefix(realRel, "..") {
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
			return fmt.Errorf("%w: %s", ErrFileNotFound, targetPath)
		}
		return fmt.Errorf("failed to stat file %s: %w", targetPath, err)
	}

	// 1. Check size
	if fi.Size() != expectedSize {
		return fmt.Errorf("%w: size changed from %d to %d bytes for %s", ErrFileModifiedOnDisk, expectedSize, fi.Size(), targetPath)
	}

	// 2. Check mtime
	if fi.ModTime().Unix() != expectedMtime {
		return fmt.Errorf("%w: mtime changed from %d to %d for %s", ErrFileModifiedOnDisk, expectedMtime, fi.ModTime().Unix(), targetPath)
	}

	// 3. Check inode / dev ID (on Linux and Unix systems)
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		if expectedInode > 0 && uint64(stat.Ino) != expectedInode {
			return fmt.Errorf("%w: inode changed from %d to %d for %s", ErrFileModifiedOnDisk, expectedInode, stat.Ino, targetPath)
		}
		if expectedDev > 0 && uint64(stat.Dev) != expectedDev {
			return fmt.Errorf("%w: device ID changed from %d to %d for %s", ErrFileModifiedOnDisk, expectedDev, stat.Dev, targetPath)
		}
	}

	return nil
}

// ValidateConfirmationToken checks for exact token "DELETE".
func (v *Validator) ValidateConfirmationToken(token string) error {
	trimmed := strings.TrimSpace(token)
	if trimmed != constants.RequiredDeleteToken {
		return fmt.Errorf("%w: received '%s'", ErrInvalidDeleteToken, trimmed)
	}
	return nil
}
