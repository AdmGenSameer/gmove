package lifecycle

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
)

// UpdateOptions configures the behavior of gmove update.
type UpdateOptions struct {
	CheckOnly bool
	Force     bool
	Stdout    io.Writer
}

// UninstallOptions configures the behavior of gmove uninstall.
type UninstallOptions struct {
	NonInteractive bool
	Purge          bool
	Stdin          io.Reader
	Stdout         io.Writer
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// CompareVersions compares two semver-like strings (e.g. "v1.0.4" vs "1.0.3").
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2.
func CompareVersions(v1, v2 string) int {
	clean := func(v string) []int {
		v = strings.TrimSpace(v)
		v = strings.TrimPrefix(v, "v")
		v = strings.TrimPrefix(v, "V")
		parts := strings.Split(v, ".")
		var nums []int
		for _, p := range parts {
			// Extract numeric prefix if there is metadata like -beta
			numStr := ""
			for _, r := range p {
				if r >= '0' && r <= '9' {
					numStr += string(r)
				} else {
					break
				}
			}
			n, _ := strconv.Atoi(numStr)
			nums = append(nums, n)
		}
		for len(nums) < 3 {
			nums = append(nums, 0)
		}
		return nums
	}

	p1 := clean(v1)
	p2 := clean(v2)

	for i := 0; i < len(p1) && i < len(p2); i++ {
		if p1[i] < p2[i] {
			return -1
		}
		if p1[i] > p2[i] {
			return 1
		}
	}
	return 0
}

// FetchLatestRelease queries the GitHub API for the latest release metadata.
func FetchLatestRelease(ctx context.Context) (*githubRelease, error) {
	url := "https://api.github.com/repos/AdmGenSameer/gmove/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gmove-updater/"+constants.Version)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, fmt.Errorf("failed to parse release JSON: %w", err)
	}
	return &rel, nil
}

// Update checks for and applies updates to the GMOVE executable.
func Update(ctx context.Context, opts UpdateOptions) error {
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}

	currentVersion := constants.Version
	fmt.Fprintf(out, "╭────────────────────────────────────────────────────────────╮\n")
	fmt.Fprintf(out, "│                       GMOVE UPDATE                         │\n")
	fmt.Fprintf(out, "╰────────────────────────────────────────────────────────────╯\n\n")
	fmt.Fprintf(out, "Current version: v%s\n", currentVersion)
	fmt.Fprintf(out, "Checking GitHub for latest release...\n")

	rel, err := FetchLatestRelease(ctx)
	if err != nil {
		fmt.Fprintf(out, "⚠ Could not fetch release from GitHub API: %v\n", err)
		if opts.CheckOnly {
			return err
		}
		fmt.Fprintf(out, "Falling back to official installer script to update...\n\n")
		return runInstallerFallback()
	}

	latestTag := rel.TagName
	cmp := CompareVersions(currentVersion, latestTag)

	if !opts.Force && cmp >= 0 {
		fmt.Fprintf(out, "✓ GMOVE is already up to date (%s).\n\n", latestTag)
		return nil
	}

	fmt.Fprintf(out, "New version available: %s (installed: v%s)\n", latestTag, currentVersion)

	if opts.CheckOnly {
		fmt.Fprintf(out, "\nRun 'gmove update' to upgrade to %s.\n", latestTag)
		return nil
	}

	// Locate current executable
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate running executable: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exePath)
	if err == nil && realExe != "" {
		exePath = realExe
	}

	// Look for matching release binary asset: gmove-<os>-<arch>.tar.gz
	expectedAsset := fmt.Sprintf("gmove-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, asset := range rel.Assets {
		if asset.Name == expectedAsset {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL != "" {
		fmt.Fprintf(out, "Downloading %s from %s...\n", expectedAsset, downloadURL)
		if err := downloadAndReplaceBinary(ctx, downloadURL, exePath); err != nil {
			fmt.Fprintf(out, "⚠ Direct binary update failed (%v). Falling back to installer...\n", err)
			return runInstallerFallback()
		}
		fmt.Fprintf(out, "\n✓ Successfully updated GMOVE to %s at %s\n\n", latestTag, exePath)
		return nil
	}

	// If asset not found on release, run install.sh fallback
	fmt.Fprintf(out, "No pre-built asset '%s' found in release %s. Running source/installer fallback...\n\n", expectedAsset, latestTag)
	return runInstallerFallback()
}

func downloadAndReplaceBinary(ctx context.Context, downloadURL, targetExe string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gmove-updater/"+constants.Version)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decompress gzip archive: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var binaryBytes []byte

	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading tar archive: %w", err)
		}

		baseName := filepath.Base(header.Name)
		if baseName == "gmove" && header.Typeflag == tar.TypeReg {
			binaryBytes, err = io.ReadAll(tarReader)
			if err != nil {
				return fmt.Errorf("failed to read binary from archive: %w", err)
			}
			break
		}
	}

	if len(binaryBytes) == 0 {
		return errors.New("gmove binary not found in downloaded tarball")
	}

	// Write to temporary file in target directory for atomic replacement
	targetDir := filepath.Dir(targetExe)
	tempFile, err := os.CreateTemp(targetDir, "gmove-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing to %s (run with sudo: sudo gmove update)", targetDir)
		}
		return err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if _, err := tempFile.Write(binaryBytes); err != nil {
		tempFile.Close()
		return err
	}
	if err := tempFile.Chmod(0755); err != nil {
		tempFile.Close()
		return err
	}
	tempFile.Close()

	// Atomic rename replaces the existing binary
	if err := os.Rename(tempPath, targetExe); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied replacing %s (run with sudo: sudo gmove update)", targetExe)
		}
		return err
	}

	return nil
}

func runInstallerFallback() error {
	cmd := exec.Command("bash", "-c", "curl -fsSL https://raw.githubusercontent.com/AdmGenSameer/gmove/main/install.sh | bash")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// Uninstall safely removes the GMOVE executable and optionally purges config, DB, and logs.
func Uninstall(opts UninstallOptions) error {
	in := opts.Stdin
	if in == nil {
		in = os.Stdin
	}
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate executable: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exePath)
	if err == nil && realExe != "" {
		exePath = realExe
	}

	configDir := filepath.Dir(constants.DefaultConfigPath())
	dbDir := filepath.Dir(constants.DefaultDatabasePath())
	stateDir := filepath.Dir(constants.DefaultLogPath())

	fmt.Fprintf(out, "╭────────────────────────────────────────────────────────────╮\n")
	fmt.Fprintf(out, "│                     GMOVE UNINSTALL                        │\n")
	fmt.Fprintf(out, "╰────────────────────────────────────────────────────────────╯\n\n")
	fmt.Fprintf(out, "The following executable will be removed:\n")
	fmt.Fprintf(out, "  • Executable: %s\n\n", exePath)

	reader := bufio.NewReader(in)

	if !opts.NonInteractive {
		fmt.Fprintf(out, "Are you sure you want to uninstall GMOVE? [y/N]: ")
		confirm, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		confirm = strings.TrimSpace(strings.ToLower(confirm))
		if confirm != "y" && confirm != "yes" {
			fmt.Fprintf(out, "\nUninstall cancelled. No files were removed.\n\n")
			return nil
		}

		if !opts.Purge {
			fmt.Fprintf(out, "\nDo you also want to delete configuration, database history, and logs? [y/N]: ")
			purgeConfirm, err := reader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			purgeConfirm = strings.TrimSpace(strings.ToLower(purgeConfirm))
			if purgeConfirm == "y" || purgeConfirm == "yes" {
				opts.Purge = true
			}
		}
	}

	// 1. Remove the binary
	if err := os.Remove(exePath); err != nil && !os.IsNotExist(err) {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied removing %s (run with sudo: sudo gmove uninstall)", exePath)
		}
		return fmt.Errorf("failed to remove executable %s: %w", exePath, err)
	}
	fmt.Fprintf(out, "\n✓ Removed binary: %s\n", exePath)

	// Check if ~/.local/bin/gmove or /usr/local/bin/gmove also exists and is an old binary
	commonPaths := []string{
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "gmove"),
		"/usr/local/bin/gmove",
	}
	for _, p := range commonPaths {
		if p != exePath {
			if _, err := os.Stat(p); err == nil {
				_ = os.Remove(p)
			}
		}
	}

	// 2. Handle data / config directories
	if opts.Purge {
		if err := os.RemoveAll(configDir); err == nil {
			fmt.Fprintf(out, "✓ Purged configuration: %s\n", configDir)
		}
		if err := os.RemoveAll(dbDir); err == nil {
			fmt.Fprintf(out, "✓ Purged database:      %s\n", dbDir)
		}
		if err := os.RemoveAll(stateDir); err == nil {
			fmt.Fprintf(out, "✓ Purged logs:          %s\n", stateDir)
		}
		fmt.Fprintf(out, "\nAll GMOVE files and state have been completely removed.\n\n")
	} else {
		fmt.Fprintf(out, "\nℹ Configuration and migration history were preserved:\n")
		fmt.Fprintf(out, "  • Config:   %s\n", configDir)
		fmt.Fprintf(out, "  • Database: %s\n", dbDir)
		fmt.Fprintf(out, "  • Logs:     %s\n", stateDir)
		fmt.Fprintf(out, "To delete them manually, run:\n")
		fmt.Fprintf(out, "  rm -rf %s %s %s\n\n", configDir, dbDir, stateDir)
		fmt.Fprintf(out, "GMOVE has been successfully uninstalled.\n\n")
	}

	return nil
}
