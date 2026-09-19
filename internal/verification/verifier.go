package verification

import (
	"context"
	"fmt"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/rclone"
)

// Verifier audits transferred files against Google Drive.
type Verifier struct {
	rcloneClient rclone.RcloneClient
	repo         *database.Repository
}

func NewVerifier(client rclone.RcloneClient, repo *database.Repository) *Verifier {
	return &Verifier{
		rcloneClient: client,
		repo:         repo,
	}
}

// VerifyItem runs rclone check on an individual transfer item and updates the database.
func (v *Verifier) VerifyItem(ctx context.Context, item *database.TransferItem, remoteDest string) error {
	_ = v.repo.UpdateItemStatus(item.ID, constants.StatusVerifying, constants.VerifUnverified, "")

	dstPath := fmt.Sprintf("%s/%s", remoteDest, item.DestinationRelPath)
	res, err := v.rcloneClient.Check(ctx, item.SourceAbsPath, dstPath, true)
	if err != nil {
		errStr := fmt.Sprintf("verification failed: %v", err)
		_ = v.repo.UpdateItemStatus(item.ID, constants.StatusFailed, constants.VerifError, errStr)
		_ = v.repo.LogEvent(&item.OperationID, "ERROR", "Verification failed", fmt.Sprintf("Item %s: %v", item.RelativePath, err))
		return err
	}

	if res.DifferCount > 0 {
		errStr := fmt.Sprintf("verification mismatch: %d differing files found", res.DifferCount)
		_ = v.repo.UpdateItemStatus(item.ID, constants.StatusFailed, constants.VerifMismatchHash, errStr)
		_ = v.repo.LogEvent(&item.OperationID, "WARN", "Verification mismatch", fmt.Sprintf("Item %s has differing files", item.RelativePath))
		return fmt.Errorf("%s", errStr)
	}

	now := time.Now().UTC()
	if err := v.repo.UpdateItemVerified(item.ID, now, "VERIFIED_OK", "VERIFIED_OK"); err != nil {
		return fmt.Errorf("failed to update verified status in db: %w", err)
	}

	_ = v.repo.LogEvent(&item.OperationID, "INFO", "Item verified successfully", fmt.Sprintf("Item: %s", item.RelativePath))
	return nil
}
