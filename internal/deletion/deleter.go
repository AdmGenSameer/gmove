package deletion

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/logger"
	"github.com/AdmGenSameer/gmove/internal/safety"
)

var (
	ErrItemNotVerified = errors.New("cannot delete: item is not in VERIFIED status")
	ErrOperationMismatch = errors.New("cannot delete: item does not belong to specified operation")
)

// DeleteRequest represents an authorized request to delete verified local files.
type DeleteRequest struct {
	OperationID       int64
	ItemIDs           []int64
	ConfirmationToken string
}

// FailedDeletion records an item that failed pre-deletion validation or unlinking.
type FailedDeletion struct {
	Item   *database.TransferItem
	Reason string
}

// DeleteResult reports the outcome of the deletion operation.
type DeleteResult struct {
	DeletedCount    int
	TotalBytesFreed int64
	DeletedItems    []*database.TransferItem
	Failed          []*FailedDeletion
}

// Deleter is the ONLY component authorized to delete source media files.
type Deleter struct {
	repo      *database.Repository
	validator *safety.Validator
	sourceDir string
}

func NewDeleter(repo *database.Repository, validator *safety.Validator, sourceDir string) *Deleter {
	return &Deleter{
		repo:      repo,
		validator: validator,
		sourceDir: filepath.Clean(sourceDir),
	}
}

// Execute processes a DeleteRequest with exhaustive pre-deletion safety checks.
func (d *Deleter) Execute(req *DeleteRequest) (*DeleteResult, error) {
	logger.For("deleter").WithOp(req.OperationID).Infof("Processing delete request for %d items", len(req.ItemIDs))

	// 1. Validate confirmation token strictly
	if err := d.validator.ValidateConfirmationToken(req.ConfirmationToken); err != nil {
		logger.For("deleter").WithOp(req.OperationID).Warnf("Deletion rejected due to invalid confirmation token: %v", err)
		return nil, fmt.Errorf("deletion rejected: %w", err)
	}

	result := &DeleteResult{
		DeletedItems: []*database.TransferItem{},
		Failed:       []*FailedDeletion{},
	}

	for _, itemID := range req.ItemIDs {
		item, err := d.repo.GetTransferItem(itemID)
		if err != nil {
			logger.For("deleter").WithOp(req.OperationID).Errorf("Failed to retrieve item #%d from db: %v", itemID, err)
			result.Failed = append(result.Failed, &FailedDeletion{
				Item:   nil,
				Reason: fmt.Sprintf("database lookup failed: %v", err),
			})
			continue
		}

		// 2. Verify item belongs to this operation
		if item.OperationID != req.OperationID {
			logger.For("deleter").WithOp(req.OperationID).Errorf("Operation mismatch for item #%d (belongs to %d, requested %d)", item.ID, item.OperationID, req.OperationID)
			result.Failed = append(result.Failed, &FailedDeletion{
				Item:   item,
				Reason: ErrOperationMismatch.Error(),
			})
			continue
		}

		// 3. Verify item status is strictly VERIFIED
		if item.Status != constants.StatusVerified {
			logger.For("deleter").WithOp(req.OperationID).Warnf("Item #%d (%s) skipped: status is %s (must be VERIFIED)", item.ID, item.RelativePath, item.Status)
			result.Failed = append(result.Failed, &FailedDeletion{
				Item:   item,
				Reason: fmt.Sprintf("%s (current status: %s)", ErrItemNotVerified.Error(), item.Status),
			})
			continue
		}

		// 4. Verify path is safely contained within sourceDir
		safePath, err := d.validator.ValidatePathSafety(item.SourceAbsPath)
		if err != nil {
			logger.For("deleter").WithOp(req.OperationID).Errorf("Path safety check failed for item #%d (%s): %v", item.ID, item.SourceAbsPath, err)
			result.Failed = append(result.Failed, &FailedDeletion{
				Item:   item,
				Reason: fmt.Sprintf("path safety check failed: %v", err),
			})
			continue
		}

		// 5. Re-stat and verify identity (size, mtime, inode) immediately before unlinking
		if err := d.validator.ValidateFileUnchanged(safePath, item.SizeBytes, item.MtimeEpoch, item.Inode, item.DeviceID); err != nil {
			logger.For("deleter").WithOp(req.OperationID).Errorf("Drift check failed before deletion for item #%d (%s): %v", item.ID, safePath, err)
			result.Failed = append(result.Failed, &FailedDeletion{
				Item:   item,
				Reason: fmt.Sprintf("liveness/drift check failed: %v", err),
			})
			continue
		}

		// 6. Delete the file from local filesystem
		if err := os.Remove(safePath); err != nil {
			logger.For("deleter").WithOp(req.OperationID).Errorf("os.Remove failed for item #%d (%s): %v", item.ID, safePath, err)
			result.Failed = append(result.Failed, &FailedDeletion{
				Item:   item,
				Reason: fmt.Sprintf("os.Remove failed: %v", err),
			})
			continue
		}

		// 7. Clean up parent directory if empty (for directory-based bundles)
		d.cleanEmptyParentDir(safePath)

		// 8. Update SQLite status to DELETED
		now := time.Now().UTC()
		if err := d.repo.UpdateItemDeleted(item.ID, now); err != nil {
			logger.For("deleter").WithOp(req.OperationID).Errorf("Failed to update item state after deletion: %v", err)
			_ = d.repo.LogEvent(&req.OperationID, "ERROR", "deleter", "Failed to update item state after physical deletion", fmt.Sprintf("item %d: %v", item.ID, err))
		}

		_ = d.repo.LogEvent(&req.OperationID, "INFO", "deleter", "Safely deleted local verified file", fmt.Sprintf("Path: %s, Size: %d bytes", safePath, item.SizeBytes))
		logger.For("deleter").WithOp(req.OperationID).Infof("Safely deleted local file: %s (%d bytes freed)", safePath, item.SizeBytes)

		result.DeletedCount++
		result.TotalBytesFreed += item.SizeBytes
		result.DeletedItems = append(result.DeletedItems, item)
	}

	logger.For("deleter").WithOp(req.OperationID).Infof("Deletion finished: %d deleted (%d bytes freed), %d failed", result.DeletedCount, result.TotalBytesFreed, len(result.Failed))
	return result, nil
}

func (d *Deleter) cleanEmptyParentDir(filePath string) {
	cleanSource := d.sourceDir
	curr := filepath.Dir(filePath)

	// Recursively clean empty parent directories up to source root (e.g. Show/Season 01/ -> Show/ -> stops at source)
	for curr != cleanSource && len(curr) > len(cleanSource) {
		rel, err := filepath.Rel(cleanSource, curr)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
			break
		}

		entries, err := os.ReadDir(curr)
		if err != nil || len(entries) > 0 {
			break // Not empty, stop
		}

		if err := os.Remove(curr); err != nil {
			break
		}
		curr = filepath.Dir(curr)
	}
}
