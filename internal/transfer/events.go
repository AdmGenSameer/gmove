package transfer

import (
	"github.com/samarcher/gmove/internal/database"
	"github.com/samarcher/gmove/internal/rclone"
)

type EventType string

const (
	EventItemStarted     EventType = "ITEM_STARTED"
	EventItemProgress    EventType = "ITEM_PROGRESS"
	EventItemTransferred EventType = "ITEM_TRANSFERRED"
	EventItemVerifying   EventType = "ITEM_VERIFYING"
	EventItemVerified    EventType = "ITEM_VERIFIED"
	EventItemFailed      EventType = "ITEM_FAILED"
	EventBatchComplete   EventType = "BATCH_COMPLETE"
)

type TransferEvent struct {
	Type          EventType
	Item          *database.TransferItem
	Stats         *rclone.TransferStats
	Error         error
	VerifiedCount int
	FailedCount   int
	TotalBytes    int64
	CompletedBytes int64
}
