package rclone

import (
	"context"
)

// TransferOptions configures rclone copy parameters.
type TransferOptions struct {
	Transfers       int
	Checkers        int
	Retries         int
	LowLevelRetries int
	DriveChunkSize  string
	DryRun          bool
}

// InProgressFile tracks a file currently being transferred by rclone.
type InProgressFile struct {
	Name       string  `json:"name"`
	Bytes      int64   `json:"bytes"`
	Size       int64   `json:"size"`
	Percentage int     `json:"percentage"`
	Speed      float64 `json:"speed"`
	ETA        int64   `json:"eta"` // Seconds remaining
}

// TransferStats captures real-time progress emitted by rclone.
type TransferStats struct {
	Bytes           int64             `json:"bytes"`
	TotalBytes      int64             `json:"totalBytes"`
	Speed           float64           `json:"speed"`
	ETA             *int64            `json:"eta"`
	Transfers       int               `json:"transfers"`
	TotalTransfers  int               `json:"totalTransfers"`
	Errors          int               `json:"errors"`
	InProgressFiles []*InProgressFile `json:"transferring"`
}

// RemoteStorageInfo reports storage details from rclone about.
type RemoteStorageInfo struct {
	TotalBytes int64
	UsedBytes  int64
	FreeBytes  int64
}

// RemoteFileItem represents a remote object returned by rclone lsjson.
type RemoteFileItem struct {
	Path     string            `json:"Path"`
	Name     string            `json:"Name"`
	Size     int64             `json:"Size"`
	MimeType string            `json:"MimeType"`
	ModTime  string            `json:"ModTime"`
	IsDir    bool              `json:"IsDir"`
	Hashes   map[string]string `json:"Hashes"` // e.g. "MD5": "..."
}

// CheckResult contains summary of rclone check.
type CheckResult struct {
	MatchCount   int
	DifferCount  int
	MissingCount int
	ErrorCount   int
	Differences  []string
	Matches      []string
}

// RcloneClient defines operations provided by rclone.
type RcloneClient interface {
	CheckExecutable(ctx context.Context) (string, error)
	ListRemotes(ctx context.Context) ([]string, error)
	AboutRemote(ctx context.Context, remote string) (*RemoteStorageInfo, error)
	ListJSON(ctx context.Context, remotePath string) ([]*RemoteFileItem, error)
	Copy(ctx context.Context, srcPath, dstRemotePath string, opts *TransferOptions, onProgress func(stats *TransferStats)) error
	Check(ctx context.Context, srcPath, dstRemotePath string, oneWay bool) (*CheckResult, error)
}
