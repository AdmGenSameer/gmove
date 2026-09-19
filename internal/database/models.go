package database

import (
	"time"

	"github.com/samarcher/gmove/internal/constants"
)

// Operation records a migration batch run.
type Operation struct {
	ID          int64            `json:"id"`
	StartedAt   time.Time        `json:"started_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	Status      constants.Status `json:"status"`
	Source      string           `json:"source"`
	Destination string           `json:"destination"`
	TotalItems  int              `json:"total_items"`
	TotalFiles  int              `json:"total_files"`
	TotalBytes  int64            `json:"total_bytes"`
	DryRun      bool             `json:"dry_run"`
	Notes       string           `json:"notes,omitempty"`
}

// TransferItem records a single media file or directory bundle within an operation.
type TransferItem struct {
	ID                 int64                        `json:"id"`
	OperationID        int64                        `json:"operation_id"`
	Name               string                       `json:"name"`
	IsDirectory        bool                         `json:"is_directory"`
	RelativePath       string                       `json:"relative_path"`
	SourceAbsPath      string                       `json:"source_abs_path"`
	DestinationRelPath string                       `json:"destination_rel_path"`
	SizeBytes          int64                        `json:"size_bytes"`
	MtimeEpoch         int64                        `json:"mtime_epoch"` // Unix nanoseconds or seconds
	Inode              uint64                       `json:"inode"`
	DeviceID           uint64                       `json:"device_id"`
	Status             constants.Status             `json:"status"`
	VerificationStatus constants.VerificationStatus `json:"verification_status"`
	SourceHash         string                       `json:"source_hash,omitempty"`
	RemoteHash         string                       `json:"remote_hash,omitempty"`
	Error              string                       `json:"error,omitempty"`
	TransferredAt      *time.Time                   `json:"transferred_at,omitempty"`
	VerifiedAt         *time.Time                   `json:"verified_at,omitempty"`
	DeletedAt          *time.Time                   `json:"deleted_at,omitempty"`
}

// Event records an audit log entry.
type Event struct {
	ID          int64     `json:"id"`
	OperationID *int64    `json:"operation_id,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
	Level       string    `json:"level"`
	Message     string    `json:"message"`
	Details     string    `json:"details,omitempty"`
}
