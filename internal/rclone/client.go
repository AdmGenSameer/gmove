package rclone

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/AdmGenSameer/gmove/internal/logger"
)

var (
	ErrRcloneNotFound       = errors.New("rclone executable not found")
	ErrQuotaExceeded        = errors.New("google drive upload quota exceeded (750 GB/day limit reached)")
	ErrRemoteNotConfigured  = errors.New("configured remote not found in rclone")
	ErrVerificationMismatch = errors.New("rclone check found differences between source and destination")
)

type SubprocessClient struct {
	binaryPath string
}

func NewSubprocessClient(binPath string) *SubprocessClient {
	if binPath == "" {
		binPath = "rclone"
	}
	return &SubprocessClient{binaryPath: binPath}
}

// CheckExecutable verifies rclone is installed and returns version string.
func (c *SubprocessClient) CheckExecutable(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "version")
	out, err := cmd.Output()
	if err != nil {
		logger.Errorf("rclone", "Executable check failed: %v", err)
		return "", fmt.Errorf("%w: please install rclone: %v", ErrRcloneNotFound, err)
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		ver := strings.TrimSpace(lines[0])
		logger.Infof("rclone", "Discovered rclone binary: %s (%s)", c.binaryPath, ver)
		return ver, nil
	}
	return "rclone", nil
}

func formatCmdError(prefix string, err error) error {
	if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
		return fmt.Errorf("%s: %s (%w)", prefix, strings.TrimSpace(string(exitErr.Stderr)), err)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}

// ListRemotes lists configured rclone remotes.
func (c *SubprocessClient) ListRemotes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "listremotes")
	out, err := cmd.Output()
	if err != nil {
		cmdErr := formatCmdError("failed to list rclone remotes", err)
		logger.Errorf("rclone", "%v", cmdErr)
		return nil, cmdErr
	}

	var remotes []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			remotes = append(remotes, strings.TrimSuffix(line, ":"))
		}
	}
	logger.Infof("rclone", "Found %d configured remotes: %s", len(remotes), strings.Join(remotes, ", "))
	return remotes, nil
}

// AboutRemote retrieves storage capacity details for the remote.
func (c *SubprocessClient) AboutRemote(ctx context.Context, remote string) (*RemoteStorageInfo, error) {
	remoteTarget := strings.TrimSuffix(remote, ":") + ":"
	cmd := exec.CommandContext(ctx, c.binaryPath, "about", remoteTarget, "--json")
	out, err := cmd.Output()
	if err != nil {
		cmdErr := formatCmdError("failed to query remote storage info", err)
		logger.Errorf("rclone", "AboutRemote failed for %s: %v", remoteTarget, cmdErr)
		return nil, cmdErr
	}

	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		logger.Errorf("rclone", "Failed to parse about JSON for %s: %v", remoteTarget, err)
		return nil, err
	}

	info := &RemoteStorageInfo{}
	if total, ok := raw["total"].(float64); ok {
		info.TotalBytes = int64(total)
	}
	if used, ok := raw["used"].(float64); ok {
		info.UsedBytes = int64(used)
	}
	if free, ok := raw["free"].(float64); ok {
		info.FreeBytes = int64(free)
	}

	logger.Infof("rclone", "Remote %s capacity - Total: %d, Used: %d, Free: %d", remoteTarget, info.TotalBytes, info.UsedBytes, info.FreeBytes)
	return info, nil
}

// ListJSON inspects remote directory contents.
func (c *SubprocessClient) ListJSON(ctx context.Context, remotePath string) ([]*RemoteFileItem, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "lsjson", remotePath, "--hash")
	out, err := cmd.Output()
	if err != nil {
		return nil, formatCmdError(fmt.Sprintf("failed to list remote path %s", remotePath), err)
	}

	var items []*RemoteFileItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, fmt.Errorf("failed to parse lsjson output: %w", err)
	}
	return items, nil
}

type rcloneLogEntry struct {
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	Stats *TransferStats `json:"stats,omitempty"`
}

// Copy invokes rclone copy with argument vectors and streams JSON stats.
func (c *SubprocessClient) Copy(ctx context.Context, srcPath, dstRemotePath string, opts *TransferOptions, onProgress func(stats *TransferStats)) error {
	args := []string{
		"copy",
		srcPath,
		dstRemotePath,
		"--stats", "500ms",
		"--stats-log-level", "NOTICE",
		"--use-json-log",
	}

	if opts != nil {
		if opts.Transfers > 0 {
			args = append(args, "--transfers", strconv.Itoa(opts.Transfers))
		}
		if opts.Checkers > 0 {
			args = append(args, "--checkers", strconv.Itoa(opts.Checkers))
		}
		if opts.Retries > 0 {
			args = append(args, "--retries", strconv.Itoa(opts.Retries))
		}
		if opts.LowLevelRetries > 0 {
			args = append(args, "--low-level-retries", strconv.Itoa(opts.LowLevelRetries))
		}
		if opts.DriveChunkSize != "" {
			args = append(args, "--drive-chunk-size", opts.DriveChunkSize)
		}
		if opts.DryRun {
			args = append(args, "--dry-run")
		}
	}

	logger.Infof("rclone", "Copy starting: %s -> %s", srcPath, dstRemotePath)
	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		logger.Errorf("rclone", "Failed to open stderr pipe: %v", err)
		return fmt.Errorf("failed to open stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("rclone", "Failed to start rclone process: %v", err)
		return fmt.Errorf("failed to start rclone process: %w", err)
	}

	scanner := bufio.NewScanner(stderrPipe)
	var lastErrorMessage string

	for scanner.Scan() {
		line := scanner.Bytes()
		var entry rcloneLogEntry
		if err := json.Unmarshal(line, &entry); err == nil {
			if entry.Stats != nil && onProgress != nil {
				onProgress(entry.Stats)
			}
			if entry.Msg != "" && opts != nil && opts.OnLog != nil {
				trimmed := strings.TrimSpace(entry.Msg)
				if !strings.HasPrefix(trimmed, "Transferred:") &&
					!strings.HasPrefix(trimmed, "Elapsed time:") &&
					!strings.HasPrefix(trimmed, "Transferring:") {
					singleLine := strings.Join(strings.Fields(trimmed), " ")
					opts.OnLog(entry.Level, singleLine)
				}
			}
			if entry.Level == "error" {
				lastErrorMessage = entry.Msg
				logger.Errorf("rclone", "rclone stderr error: %s", entry.Msg)
				if strings.Contains(entry.Msg, "userRateLimitExceeded") || strings.Contains(entry.Msg, "quotaExceeded") {
					logger.Errorf("rclone", "750GB daily Google Drive quota or API rate limit exceeded: %s", entry.Msg)
					return ErrQuotaExceeded
				}
			}
		} else {
			text := strings.TrimSpace(string(line))
			if text != "" && opts != nil && opts.OnLog != nil {
				if !strings.HasPrefix(text, "Transferred:") &&
					!strings.HasPrefix(text, "Elapsed time:") &&
					!strings.HasPrefix(text, "Transferring:") {
					singleLine := strings.Join(strings.Fields(text), " ")
					opts.OnLog("info", singleLine)
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			logger.Warnf("rclone", "Copy canceled by context: %s -> %s", srcPath, dstRemotePath)
			return context.Canceled
		}
		if lastErrorMessage != "" {
			logger.Errorf("rclone", "Copy failed with message (%s -> %s): %s (%v)", srcPath, dstRemotePath, lastErrorMessage, err)
			return fmt.Errorf("rclone copy failed: %s: %w", lastErrorMessage, err)
		}
		logger.Errorf("rclone", "Copy failed (%s -> %s): %v", srcPath, dstRemotePath, err)
		return fmt.Errorf("rclone copy failed: %w", err)
	}

	logger.Infof("rclone", "Copy completed successfully: %s -> %s", srcPath, dstRemotePath)
	return nil
}

// Check compares source and destination files using rclone check --one-way.
func (c *SubprocessClient) Check(ctx context.Context, srcPath, dstRemotePath string, oneWay bool) (*CheckResult, error) {
	logger.Infof("rclone", "Check starting: %s vs %s (oneWay=%v)", srcPath, dstRemotePath, oneWay)
	args := []string{
		"check",
		srcPath,
		dstRemotePath,
		"--use-json-log",
	}
	if oneWay {
		args = append(args, "--one-way")
	}

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		logger.Errorf("rclone", "Failed to open stderr pipe for check: %v", err)
		return nil, fmt.Errorf("failed to open stderr pipe for check: %w", err)
	}

	if err := cmd.Start(); err != nil {
		logger.Errorf("rclone", "Failed to start rclone check: %v", err)
		return nil, fmt.Errorf("failed to start rclone check: %w", err)
	}

	res := &CheckResult{}
	scanner := bufio.NewScanner(stderrPipe)
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry rcloneLogEntry
		if err := json.Unmarshal(line, &entry); err == nil {
			msg := entry.Msg
			if strings.Contains(msg, "differences found") {
				var count int
				_, _ = fmt.Sscanf(msg, "%d differences found", &count)
				res.DifferCount = count
			} else if strings.Contains(msg, "matching files") {
				var count int
				_, _ = fmt.Sscanf(msg, "%d matching files", &count)
				res.MatchCount = count
			}
		}
	}

	cmdErr := cmd.Wait()
	if cmdErr != nil {
		if res.DifferCount > 0 {
			logger.Warnf("rclone", "Check verification mismatch: %d differing files found between %s and %s", res.DifferCount, srcPath, dstRemotePath)
			return res, fmt.Errorf("%w: %d differing files found", ErrVerificationMismatch, res.DifferCount)
		}
		logger.Errorf("rclone", "rclone check returned error: %v", cmdErr)
		return res, fmt.Errorf("rclone check returned error: %w", cmdErr)
	}

	logger.Infof("rclone", "Check verified: %d matching files, 0 differences between %s and %s", res.MatchCount, srcPath, dstRemotePath)
	return res, nil
}
