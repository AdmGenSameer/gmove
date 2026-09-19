package utils

import (
	"fmt"
	"syscall"
	"time"
)

// FormatBytes formats byte count into human-readable string (GB, MB, KB).
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// FormatSpeed formats transfer rate in MiB/s or KiB/s.
func FormatSpeed(bytesPerSec float64) string {
	if bytesPerSec <= 0 {
		return "0 B/s"
	}
	const mib = 1024 * 1024
	const kib = 1024
	if bytesPerSec >= mib {
		return fmt.Sprintf("%.1f MiB/s", bytesPerSec/mib)
	}
	if bytesPerSec >= kib {
		return fmt.Sprintf("%.1f KiB/s", bytesPerSec/kib)
	}
	return fmt.Sprintf("%.0f B/s", bytesPerSec)
}

// FormatDuration formats duration nicely (e.g. 1m 31s).
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// FormatETA converts seconds remaining to human string.
func FormatETA(seconds int64) string {
	if seconds < 0 {
		return "-"
	}
	return FormatDuration(time.Duration(seconds) * time.Second)
}

// DiskSpace contains filesystem storage capacity.
type DiskSpace struct {
	TotalBytes uint64
	FreeBytes  uint64
	UsedBytes  uint64
}

// GetDiskSpace queries filesystem free and total space for a path.
func GetDiskSpace(path string) (*DiskSpace, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free

	return &DiskSpace{
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  used,
	}, nil
}
