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
		return "", fmt.Errorf("%w: please install rclone: %v", ErrRcloneNotFound, err)
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0]), nil
	}
	return "rclone", nil
}

// ListRemotes lists configured rclone remotes.
func (c *SubprocessClient) ListRemotes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "listremotes")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list rclone remotes: %w", err)
	}

	var remotes []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			remotes = append(remotes, strings.TrimSuffix(line, ":"))
		}
	}
	return remotes, nil
}

// AboutRemote retrieves storage capacity details for the remote.
func (c *SubprocessClient) AboutRemote(ctx context.Context, remote string) (*RemoteStorageInfo, error) {
	remoteTarget := strings.TrimSuffix(remote, ":") + ":"
	cmd := exec.CommandContext(ctx, c.binaryPath, "about", remoteTarget, "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to query remote storage info: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
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

	return info, nil
}

// ListJSON inspects remote directory contents.
func (c *SubprocessClient) ListJSON(ctx context.Context, remotePath string) ([]*RemoteFileItem, error) {
	cmd := exec.CommandContext(ctx, c.binaryPath, "lsjson", remotePath, "--hash")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list remote path %s: %w", remotePath, err)
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

	cmd := exec.CommandContext(ctx, c.binaryPath, args...)

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to open stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
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
			if entry.Level == "error" {
				lastErrorMessage = entry.Msg
				if strings.Contains(entry.Msg, "userRateLimitExceeded") || strings.Contains(entry.Msg, "quotaExceeded") {
					return ErrQuotaExceeded
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return context.Canceled
		}
		if lastErrorMessage != "" {
			return fmt.Errorf("rclone copy failed: %s: %w", lastErrorMessage, err)
		}
		return fmt.Errorf("rclone copy failed: %w", err)
	}

	return nil
}

// Check compares source and destination files using rclone check --one-way.
func (c *SubprocessClient) Check(ctx context.Context, srcPath, dstRemotePath string, oneWay bool) (*CheckResult, error) {
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
		return nil, fmt.Errorf("failed to open stderr pipe for check: %w", err)
	}

	if err := cmd.Start(); err != nil {
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
			return res, fmt.Errorf("%w: %d differing files found", ErrVerificationMismatch, res.DifferCount)
		}
		return res, fmt.Errorf("rclone check returned error: %w", cmdErr)
	}

	return res, nil
}
