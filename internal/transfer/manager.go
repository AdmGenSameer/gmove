package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AdmGenSameer/gmove/internal/config"
	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/rclone"
	"github.com/AdmGenSameer/gmove/internal/scanner"
	"github.com/AdmGenSameer/gmove/internal/utils"
	"github.com/AdmGenSameer/gmove/internal/verification"
)

// Manager coordinates pre-flight, transfers, and post-transfer verification.
type Manager struct {
	cfg          *config.Config
	repo         *database.Repository
	rcloneClient rclone.RcloneClient
	verifier     *verification.Verifier
}

func NewManager(cfg *config.Config, repo *database.Repository, client rclone.RcloneClient) *Manager {
	return &Manager{
		cfg:          cfg,
		repo:         repo,
		rcloneClient: client,
		verifier:     verification.NewVerifier(client, repo),
	}
}

// PreflightCheck verifies rclone installation and remote connectivity.
func (m *Manager) PreflightCheck(ctx context.Context) error {
	_, err := m.rcloneClient.CheckExecutable(ctx)
	if err != nil {
		return err
	}

	remotes, err := m.rcloneClient.ListRemotes(ctx)
	if err != nil {
		return fmt.Errorf("failed to query rclone remotes: %w", err)
	}

	targetRemote := strings.TrimSuffix(m.cfg.Remote, ":")
	found := false
	for _, r := range remotes {
		if strings.TrimSuffix(r, ":") == targetRemote {
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("%w: remote '%s' is not in configured remotes (%v)", rclone.ErrRemoteNotConfigured, targetRemote, remotes)
	}

	return nil
}

// PrepareOperation records an operation and its selected items in the database.
func (m *Manager) PrepareOperation(selected []*scanner.MediaItem, dryRun bool) (*database.Operation, []*database.TransferItem, error) {
	var totalBytes int64
	var totalFiles int
	for _, item := range selected {
		totalBytes += item.SizeBytes
		totalFiles += item.FileCount
	}

	op := &database.Operation{
		StartedAt:   time.Now().UTC(),
		Status:      constants.StatusPending,
		Source:      m.cfg.Source,
		Destination: m.cfg.RemoteDestination(),
		TotalItems:  len(selected),
		TotalFiles:  totalFiles,
		TotalBytes:  totalBytes,
		DryRun:      dryRun,
	}

	opID, err := m.repo.CreateOperation(op)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create operation in database: %w", err)
	}

	var dbItems []*database.TransferItem
	for _, item := range selected {
		dbItems = append(dbItems, &database.TransferItem{
			OperationID:        opID,
			Name:               item.Name,
			IsDirectory:        item.IsDirectory,
			RelativePath:       item.RelativePath,
			SourceAbsPath:      item.SourceAbsPath,
			DestinationRelPath: item.RelativePath,
			SizeBytes:          item.SizeBytes,
			MtimeEpoch:         item.MtimeEpoch,
			Inode:              item.Inode,
			DeviceID:           item.DeviceID,
			Status:             constants.StatusPending,
			VerificationStatus: constants.VerifUnverified,
		})
	}

	if err := m.repo.AddTransferItems(dbItems); err != nil {
		return nil, nil, fmt.Errorf("failed to record transfer items: %w", err)
	}

	return op, dbItems, nil
}

// Execute processes the items for an operation.
func (m *Manager) Execute(ctx context.Context, opID int64, eventCallback func(TransferEvent)) error {
	op, err := m.repo.GetOperation(opID)
	if err != nil {
		return err
	}

	if err := m.repo.UpdateOperationStatus(opID, constants.StatusRunning, nil); err != nil {
		return err
	}

	items, err := m.repo.GetTransferItems(opID)
	if err != nil {
		return err
	}

	var verifiedCount, failedCount int
	var completedBytes int64

	remoteDest := m.cfg.RemoteDestination()
	opts := &rclone.TransferOptions{
		Transfers:       m.cfg.Transfers,
		Checkers:        m.cfg.Checkers,
		Retries:         m.cfg.Retries,
		LowLevelRetries: m.cfg.LowLevelRetries,
		DriveChunkSize:  m.cfg.DriveChunkSize,
		DryRun:          op.DryRun,
	}

	opts.OnLog = func(level, msg string) {
		if eventCallback != nil && strings.TrimSpace(msg) != "" {
			eventCallback(TransferEvent{
				Type:           EventLogMessage,
				Message:        fmt.Sprintf("[rclone] %s", msg),
				VerifiedCount:  verifiedCount,
				FailedCount:    failedCount,
				TotalBytes:     op.TotalBytes,
				CompletedBytes: completedBytes,
			})
		}
	}

	defer func() {
		// If context was cancelled, mark INTERRUPTED
		if errors.Is(ctx.Err(), context.Canceled) {
			_ = m.repo.UpdateOperationStatus(opID, constants.StatusInterrupted, nil)
			_ = m.repo.LogEvent(&opID, "WARN", "Operation interrupted by user", "")
		}
	}()

	if eventCallback != nil {
		eventCallback(TransferEvent{
			Type:           EventLogMessage,
			Message:        fmt.Sprintf("[BATCH] Processing %d items (%s)", len(items), utils.FormatBytes(op.TotalBytes)),
			VerifiedCount:  verifiedCount,
			FailedCount:    failedCount,
			TotalBytes:     op.TotalBytes,
			CompletedBytes: completedBytes,
		})
	}

	for idx, item := range items {
		// Skip already verified or deleted items
		if item.Status == constants.StatusVerified || item.Status == constants.StatusDeleted {
			verifiedCount++
			completedBytes += item.SizeBytes
			continue
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Notify item started
		if eventCallback != nil {
			eventCallback(TransferEvent{
				Type:           EventItemStarted,
				Item:           item,
				Message:        fmt.Sprintf("[START %d/%d] %s (%s)", idx+1, len(items), item.Name, utils.FormatBytes(item.SizeBytes)),
				VerifiedCount:  verifiedCount,
				FailedCount:    failedCount,
				TotalBytes:     op.TotalBytes,
				CompletedBytes: completedBytes,
			})
		}

		_ = m.repo.UpdateItemStatus(item.ID, constants.StatusTransferring, constants.VerifUnverified, "")

		// Destination path on remote
		var targetDst string
		if item.IsDirectory {
			targetDst = fmt.Sprintf("%s/%s", remoteDest, item.DestinationRelPath)
		} else {
			targetDst = remoteDest
		}

		// Execute copy
		copyErr := m.rcloneClient.Copy(ctx, item.SourceAbsPath, targetDst, opts, func(stats *rclone.TransferStats) {
			if eventCallback != nil {
				eventCallback(TransferEvent{
					Type:           EventItemProgress,
					Item:           item,
					Stats:          stats,
					VerifiedCount:  verifiedCount,
					FailedCount:    failedCount,
					TotalBytes:     op.TotalBytes,
					CompletedBytes: completedBytes,
				})
			}
		})

		if copyErr != nil {
			failedCount++
			errStr := copyErr.Error()
			_ = m.repo.UpdateItemStatus(item.ID, constants.StatusFailed, constants.VerifError, errStr)
			_ = m.repo.LogEvent(&opID, "ERROR", "Transfer failed", fmt.Sprintf("Item %s: %s", item.RelativePath, errStr))

			if eventCallback != nil {
				eventCallback(TransferEvent{
					Type:           EventItemFailed,
					Item:           item,
					Error:          copyErr,
					Message:        fmt.Sprintf("[FAILED] ✗ %s: %s", item.Name, errStr),
					VerifiedCount:  verifiedCount,
					FailedCount:    failedCount,
					TotalBytes:     op.TotalBytes,
					CompletedBytes: completedBytes,
				})
			}
			continue
		}

		// Transfer completed: transition to TRANSFERRED
		now := time.Now().UTC()
		_ = m.repo.UpdateItemTransferred(item.ID, now)
		if eventCallback != nil {
			eventCallback(TransferEvent{
				Type:           EventItemTransferred,
				Item:           item,
				Message:        fmt.Sprintf("[UPLOADED] %s ✓", item.Name),
				VerifiedCount:  verifiedCount,
				FailedCount:    failedCount,
				TotalBytes:     op.TotalBytes,
				CompletedBytes: completedBytes,
			})
		}

		// Immediately verify
		if eventCallback != nil {
			eventCallback(TransferEvent{
				Type:           EventItemVerifying,
				Item:           item,
				Message:        fmt.Sprintf("[VERIFY] Checking Google Drive MD5 for %s...", item.Name),
				VerifiedCount:  verifiedCount,
				FailedCount:    failedCount,
				TotalBytes:     op.TotalBytes,
				CompletedBytes: completedBytes,
			})
		}

		verifErr := m.verifier.VerifyItem(ctx, item, remoteDest)
		if verifErr != nil {
			failedCount++
			if eventCallback != nil {
				eventCallback(TransferEvent{
					Type:           EventItemFailed,
					Item:           item,
					Error:          verifErr,
					Message:        fmt.Sprintf("[FAILED] ✗ Verification mismatch for %s: %v", item.Name, verifErr),
					VerifiedCount:  verifiedCount,
					FailedCount:    failedCount,
					TotalBytes:     op.TotalBytes,
					CompletedBytes: completedBytes,
				})
			}
		} else {
			verifiedCount++
			completedBytes += item.SizeBytes
			if eventCallback != nil {
				eventCallback(TransferEvent{
					Type:           EventItemVerified,
					Item:           item,
					Message:        fmt.Sprintf("[VERIFIED] ✓ %s (checksum matched)", item.Name),
					VerifiedCount:  verifiedCount,
					FailedCount:    failedCount,
					TotalBytes:     op.TotalBytes,
					CompletedBytes: completedBytes,
				})
			}
		}
	}

	if eventCallback != nil {
		eventCallback(TransferEvent{
			Type:           EventBatchComplete,
			Message:        fmt.Sprintf("[COMPLETE] Batch finished: %d verified, %d failed", verifiedCount, failedCount),
			VerifiedCount:  verifiedCount,
			FailedCount:    failedCount,
			TotalBytes:     op.TotalBytes,
			CompletedBytes: completedBytes,
		})
	}

	// Final status update
	now := time.Now().UTC()
	var finalStatus constants.Status
	if failedCount == 0 {
		finalStatus = constants.StatusCompleted
	} else {
		finalStatus = constants.StatusCompletedWithErrors
	}
	_ = m.repo.UpdateOperationStatus(opID, finalStatus, &now)

	if eventCallback != nil {
		eventCallback(TransferEvent{
			Type:           EventBatchComplete,
			VerifiedCount:  verifiedCount,
			FailedCount:    failedCount,
			TotalBytes:     op.TotalBytes,
			CompletedBytes: completedBytes,
		})
	}

	return nil
}

// RetryFailed restarts failed items for an operation.
func (m *Manager) RetryFailed(ctx context.Context, opID int64, eventCallback func(TransferEvent)) error {
	failedItems, err := m.repo.GetFailedItems(opID)
	if err != nil {
		return err
	}
	if len(failedItems) == 0 {
		return fmt.Errorf("no failed items found for operation #%d", opID)
	}

	// Reset status to PENDING
	for _, item := range failedItems {
		_ = m.repo.UpdateItemStatus(item.ID, constants.StatusPending, constants.VerifUnverified, "")
	}

	return m.Execute(ctx, opID, eventCallback)
}
