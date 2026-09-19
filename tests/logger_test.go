package tests

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/logger"
)

func TestLoggerAndEventQueries(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_logging.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	logger.Init(repo)
	defer logger.Close()

	opID, err := repo.CreateOperation(&database.Operation{
		StartedAt:   time.Now().UTC(),
		Status:      "RUNNING",
		Source:      tempDir,
		Destination: "gdrive:movies",
		TotalItems:  1,
		TotalFiles:  1,
	})
	if err != nil {
		t.Fatalf("failed to create operation: %v", err)
	}

	// 1. Emit various logs
	logger.Infof("cli", "Application started with %s", "config.toml")
	logger.For("scanner").Infof("Discovered %d movies in library", 15)
	logger.For("rclone").Warn("Connection retry initiated")
	logger.For("safety").WithOp(opID).Errorf("Symlink traversal rejected: %s", "/etc/shadow")
	logger.For("transfer").WithOp(opID).Info("Transfer chunk completed")
	logger.For("verifier").WithOp(opID).Info("Checksum MD5 verified")
	logger.For("deleter").WithOp(opID).Infof("Safely deleted item %d", 101)

	// 2. Flush to SQLite
	logger.Flush()

	// 3. Query all logs
	allRecords, err := repo.QueryEvents(database.EventFilter{Limit: 100})
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	if len(allRecords) < 7 {
		t.Fatalf("expected at least 7 logs, got %d", len(allRecords))
	}

	// 4. Query only errors
	errRecords, err := repo.QueryEvents(database.EventFilter{OnlyErrors: true})
	if err != nil {
		t.Fatalf("failed to query error events: %v", err)
	}
	if len(errRecords) != 2 { // 1 WARN ("rclone"), 1 ERROR ("safety")
		t.Fatalf("expected 2 error/warn logs, got %d", len(errRecords))
	}

	// 5. Query by component
	rcloneRecords, err := repo.QueryEvents(database.EventFilter{Component: "rclone"})
	if err != nil {
		t.Fatalf("failed to query rclone events: %v", err)
	}
	if len(rcloneRecords) != 1 || rcloneRecords[0].Component != "rclone" {
		t.Fatalf("expected 1 rclone record, got %d", len(rcloneRecords))
	}

	// 6. Query by operation ID
	opRecords, err := repo.QueryEvents(database.EventFilter{OperationID: &opID})
	if err != nil {
		t.Fatalf("failed to query op events: %v", err)
	}
	if len(opRecords) != 4 {
		t.Fatalf("expected 4 events for op#%d, got %d", opID, len(opRecords))
	}
}

func TestConcurrentLogging(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_concurrent.db")

	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	logger.Init(repo)
	defer logger.Close()

	var wg sync.WaitGroup
	workers := 10
	logsPerWorker := 30

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < logsPerWorker; i++ {
				logger.For("worker").Infof("Worker %d logged event %d", workerID, i)
				time.Sleep(1 * time.Millisecond)
			}
		}(w)
	}

	wg.Wait()
	logger.Flush()

	records, err := repo.QueryEvents(database.EventFilter{Component: "worker", Limit: 500})
	if err != nil {
		t.Fatalf("failed to query concurrent logs: %v", err)
	}

	expectedCount := workers * logsPerWorker
	if len(records) != expectedCount {
		t.Fatalf("expected %d records, got %d", expectedCount, len(records))
	}
}
