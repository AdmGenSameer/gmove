package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/samarcher/gmove/internal/constants"
)

// Profile represents a named migration preset (e.g. "movies", "shows").
type Profile struct {
	Source     string `toml:"source"`
	Remote     string `toml:"remote"`
	RemotePath string `toml:"remote_path"`
}

// Config represents the application configuration.
type Config struct {
	Source                     string             `toml:"source"`
	Remote                     string             `toml:"remote"`
	RemotePath                 string             `toml:"remote_path"`
	Database                   string             `toml:"database"`
	LogFile                    string             `toml:"log_file"`
	MinimumFreeSpaceWarningGB  int                `toml:"minimum_free_space_warning_gb"`
	Transfers                  int                `toml:"transfers"`
	Checkers                   int                `toml:"checkers"`
	Retries                    int                `toml:"retries"`
	LowLevelRetries            int                `toml:"low_level_retries"`
	DriveChunkSize             string             `toml:"drive_chunk_size"`
	DeleteAfterVerify          bool               `toml:"delete_after_verify"`
	IgnoreDirs                 []string           `toml:"ignore_dirs"`
	Profiles                   map[string]Profile `toml:"profiles"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Source:                    "",
		Remote:                    "",
		RemotePath:                "Movies",
		Database:                  constants.DefaultDatabasePath(),
		LogFile:                   constants.DefaultLogPath(),
		MinimumFreeSpaceWarningGB: 100,
		Transfers:                 4,
		Checkers:                  8,
		Retries:                   3,
		LowLevelRetries:           10,
		DriveChunkSize:            "64M",
		DeleteAfterVerify:         false,
		IgnoreDirs:                constants.DefaultIgnoreDirs,
		Profiles:                  make(map[string]Profile),
	}
}

// ApplyProfile overrides source, remote, and remote_path from a named profile.
func (c *Config) ApplyProfile(name string) error {
	if c.Profiles == nil {
		return fmt.Errorf("no profiles configured in config.toml")
	}
	p, ok := c.Profiles[strings.ToLower(name)]
	if !ok {
		var available []string
		for k := range c.Profiles {
			available = append(available, k)
		}
		return fmt.Errorf("profile '%s' not found (available: %v)", name, available)
	}
	if p.Source != "" {
		c.Source = p.Source
	}
	if p.Remote != "" {
		c.Remote = p.Remote
	}
	if p.RemotePath != "" {
		c.RemotePath = p.RemotePath
	}
	return c.ExpandPaths()
}

// RemoteDestination returns the full remote path formatted for rclone (e.g., "gdrive:Movies").
func (c *Config) RemoteDestination() string {
	remote := strings.TrimSuffix(c.Remote, ":")
	if c.RemotePath == "" {
		return fmt.Sprintf("%s:", remote)
	}
	return fmt.Sprintf("%s:%s", remote, strings.TrimPrefix(c.RemotePath, "/"))
}

// ExpandPaths expands `~` to user's home directory in paths.
func (c *Config) ExpandPaths() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") || p == "~" {
			return filepath.Join(home, p[1:])
		}
		return p
	}

	c.Source = expand(c.Source)
	c.Database = expand(c.Database)
	c.LogFile = expand(c.LogFile)

	return nil
}

// Validate checks that required fields are present and valid.
func (c *Config) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("source directory is required")
	}
	if !filepath.IsAbs(c.Source) {
		return fmt.Errorf("source directory must be an absolute path: %s", c.Source)
	}
	if c.Remote == "" {
		return fmt.Errorf("remote is required (e.g. 'gdrive')")
	}
	if c.Database == "" {
		return fmt.Errorf("database path is required")
	}
	if c.Transfers <= 0 {
		c.Transfers = 4
	}
	if c.Checkers <= 0 {
		c.Checkers = 8
	}
	return nil
}

// Save writes the configuration to the specified TOML file.
func (c *Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config to TOML: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", path, err)
	}

	return nil
}
