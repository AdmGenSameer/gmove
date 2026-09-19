package scanner

import (
	"time"
)

// FileDetail represents an individual file on disk.
type FileDetail struct {
	RelativePath  string    // Path relative to source root
	SourceAbsPath string    // Absolute path on disk
	SizeBytes     int64     // Size in bytes
	ModTime       time.Time // Modification time
	MtimeEpoch    int64     // Unix seconds
	Inode         uint64    // Linux inode number
	DeviceID      uint64    // Linux device ID
	Hardlinks     uint64    // Hardlink count (st_nlink)
	IsMedia       bool      // True if primary media (.mkv, .mp4, etc.)
}

// MediaItem represents a logical migration unit (either a standalone video file or a movie folder bundle).
type MediaItem struct {
	Name          string        // Display name
	IsDirectory   bool          // True if a folder bundle
	RelativePath  string        // Path relative to source root
	SourceAbsPath string        // Absolute path
	SizeBytes     int64         // Total size in bytes
	FileCount     int           // Number of files (1 for single file, N for directory)
	ModTime       time.Time     // Latest modification time
	MtimeEpoch    int64         // Unix timestamp
	Inode         uint64        // Inode of the file or root directory
	DeviceID      uint64        // Device ID
	Hardlinks     uint64        // Max hardlinks among files (warns if > 1)
	Files         []*FileDetail // Detailed list of contained files
}
