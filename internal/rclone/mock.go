package rclone

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockClient provides an in-memory implementation of RcloneClient for testing.
type MockClient struct {
	mu           sync.Mutex
	Remotes      []string
	RemoteFiles  map[string]*RemoteFileItem
	ShouldFail   bool
	FailCheck    bool
	FailQuota    bool
	RecordedArgs [][]string
}

func NewMockClient() *MockClient {
	return &MockClient{
		Remotes:     []string{"gdrive", "backup"},
		RemoteFiles: make(map[string]*RemoteFileItem),
	}
}

func (m *MockClient) CheckExecutable(ctx context.Context) (string, error) {
	if m.ShouldFail {
		return "", ErrRcloneNotFound
	}
	return "rclone v1.75.0-mock", nil
}

func (m *MockClient) ListRemotes(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ShouldFail {
		return nil, fmt.Errorf("mock error listing remotes")
	}
	return m.Remotes, nil
}

func (m *MockClient) AboutRemote(ctx context.Context, remote string) (*RemoteStorageInfo, error) {
	if m.ShouldFail {
		return nil, fmt.Errorf("mock error querying remote")
	}
	return &RemoteStorageInfo{
		TotalBytes: 15 * 1024 * 1024 * 1024 * 1024,
		UsedBytes:  5 * 1024 * 1024 * 1024 * 1024,
		FreeBytes:  10 * 1024 * 1024 * 1024 * 1024,
	}, nil
}

func (m *MockClient) ListJSON(ctx context.Context, remotePath string) ([]*RemoteFileItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*RemoteFileItem
	for path, item := range m.RemoteFiles {
		if strings.HasPrefix(path, remotePath) {
			list = append(list, item)
		}
	}
	return list, nil
}

func (m *MockClient) Copy(ctx context.Context, srcPath, dstRemotePath string, opts *TransferOptions, onProgress func(stats *TransferStats)) error {
	m.mu.Lock()
	m.RecordedArgs = append(m.RecordedArgs, []string{"copy", srcPath, dstRemotePath})
	m.mu.Unlock()

	if m.FailQuota {
		return ErrQuotaExceeded
	}
	if m.ShouldFail {
		return fmt.Errorf("mock copy error")
	}

	// Simulate progress callback
	if onProgress != nil {
		eta := int64(10)
		onProgress(&TransferStats{
			Bytes:      1000,
			TotalBytes: 1000,
			Speed:      1024 * 1024 * 50,
			ETA:        &eta,
			Transfers:  1,
		})
	}

	m.mu.Lock()
	m.RemoteFiles[dstRemotePath] = &RemoteFileItem{
		Path:    dstRemotePath,
		Size:    1000,
		ModTime: time.Now().Format(time.RFC3339),
		Hashes:  map[string]string{"MD5": "mockmd5sum123"},
	}
	m.mu.Unlock()

	return nil
}

func (m *MockClient) Check(ctx context.Context, srcPath, dstRemotePath string, oneWay bool) (*CheckResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailCheck {
		return &CheckResult{DifferCount: 1}, ErrVerificationMismatch
	}

	return &CheckResult{
		MatchCount: 1,
	}, nil
}
