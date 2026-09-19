package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/AdmGenSameer/gmove/internal/config"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/logger"
	"github.com/AdmGenSameer/gmove/internal/rclone"
	"github.com/AdmGenSameer/gmove/internal/transfer"
)

// SpawnWorker spawns an independent, detached background worker process to execute an operation.
// It sets Setsid=true to decouple from the terminal session so SSH disconnects won't stop it.
func SpawnWorker(opID int64, configPath string) (int, error) {
	exePath, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to locate current executable: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exePath)
	if err == nil && realExe != "" {
		exePath = realExe
	}

	args := []string{"--worker-op", strconv.FormatInt(opID, 10)}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	cmd := exec.Command(exePath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Creates a new session; detaches from controlling TTY
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdin = devNull
		cmd.Stdout = devNull
		cmd.Stderr = devNull
	}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start background worker: %w", err)
	}

	return cmd.Process.Pid, nil
}

// IsProcessAlive checks whether the process with the given PID is currently active.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, sending signal 0 checks for process existence without affecting it
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// StopWorker sends a graceful SIGTERM signal to terminate the background worker.
func StopWorker(pid int) error {
	if pid <= 0 {
		return errors.New("invalid PID")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

// RunWorker executes the operation in a headless background daemon process.
func RunWorker(ctx context.Context, opID int64, cfg *config.Config, repo *database.Repository) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Ignore SIGHUP so terminal closures/disconnections never kill this process
	signal.Ignore(syscall.SIGHUP)

	// Listen for SIGINT and SIGTERM for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigChan:
			logger.For("daemon").WithOp(opID).Warnf("Daemon worker received signal %v, shutting down gracefully", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	pid := os.Getpid()
	if err := repo.UpdateOperationPID(opID, pid); err != nil {
		logger.For("daemon").WithOp(opID).Errorf("Failed to record worker PID %d: %v", pid, err)
	}

	rcloneClient := rclone.NewSubprocessClient("")
	manager := transfer.NewManager(cfg, repo, rcloneClient)

	execErr := manager.Execute(ctx, opID, nil)

	// Clean up PID record when done
	_ = repo.UpdateOperationPID(opID, 0)
	logger.Flush()

	if execErr != nil {
		logger.For("daemon").WithOp(opID).Errorf("Background operation #%d completed with error: %v", opID, execErr)
		return execErr
	}

	logger.For("daemon").WithOp(opID).Infof("Background operation #%d completed successfully", opID)
	return nil
}
