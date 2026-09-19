package tests

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/daemon"
	"github.com/AdmGenSameer/gmove/internal/database"
)

func TestDaemonProcessLiveness(t *testing.T) {
	// Current process PID should always be alive
	currentPID := os.Getpid()
	if !daemon.IsProcessAlive(currentPID) {
		t.Fatalf("expected current PID %d to be reported alive", currentPID)
	}

	// Invalid PIDs should report false
	if daemon.IsProcessAlive(0) {
		t.Fatalf("expected PID 0 to report not alive")
	}
	if daemon.IsProcessAlive(-1) {
		t.Fatalf("expected negative PID to report not alive")
	}
	// A PID that practically does not exist
	if daemon.IsProcessAlive(9999999) {
		t.Fatalf("expected high PID 9999999 to report not alive")
	}
}

func TestDaemonStopInvalidPID(t *testing.T) {
	if err := daemon.StopWorker(0); err == nil {
		t.Fatal("expected error when stopping PID 0")
	}
	if err := daemon.StopWorker(-5); err == nil {
		t.Fatal("expected error when stopping negative PID")
	}
}

func TestDaemonDatabasePIDTracking(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_daemon.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)

	// Create operation
	op := &database.Operation{
		StartedAt:   time.Now(),
		Status:      constants.StatusRunning,
		Source:      "/media/test",
		Destination: "gdrive:Media",
		TotalItems:  5,
		TotalBytes:  1024 * 1024,
	}
	opID, err := repo.CreateOperation(op)
	if err != nil {
		t.Fatalf("failed to create operation: %v", err)
	}

	readOp, err := repo.GetOperation(opID)
	if err != nil {
		t.Fatalf("failed to read created operation: %v", err)
	}
	if readOp.PID != 0 {
		t.Fatalf("expected initial PID to be 0, got %d", readOp.PID)
	}

	// No running operation initially
	running, err := repo.GetRunningOperation()
	if err != nil {
		t.Fatalf("failed to query running operation: %v", err)
	}
	if running != nil {
		t.Fatalf("expected no running operation initially, got #%d", running.ID)
	}

	// Update PID
	currentPID := os.Getpid()
	if err := repo.UpdateOperationPID(opID, currentPID); err != nil {
		t.Fatalf("failed to update operation PID: %v", err)
	}

	// Read back operation
	updatedOp, err := repo.GetOperation(opID)
	if err != nil {
		t.Fatalf("failed to get operation: %v", err)
	}
	if updatedOp.PID != currentPID {
		t.Fatalf("expected PID %d, got %d", currentPID, updatedOp.PID)
	}

	// Now GetRunningOperation should return this operation
	running, err = repo.GetRunningOperation()
	if err != nil {
		t.Fatalf("failed to query running operation: %v", err)
	}
	if running == nil {
		t.Fatalf("expected running operation with PID %d, got nil", currentPID)
	}
	if running.ID != opID || running.PID != currentPID {
		t.Fatalf("mismatched running operation: ID=%d, PID=%d", running.ID, running.PID)
	}

	// Clean up PID
	if err := repo.UpdateOperationPID(opID, 0); err != nil {
		t.Fatalf("failed to clear PID: %v", err)
	}

	running, err = repo.GetRunningOperation()
	if err != nil {
		t.Fatalf("failed to query running operation after clear: %v", err)
	}
	if running != nil {
		t.Fatalf("expected running operation to be nil after clear, got #%d", running.ID)
	}

	// Mark status completed
	if err := repo.UpdateOperationStatus(opID, constants.StatusCompleted, nil); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}
}
