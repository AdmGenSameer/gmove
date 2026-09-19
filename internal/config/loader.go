package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/AdmGenSameer/gmove/internal/constants"
)

var ErrConfigNotFound = errors.New("configuration file not found")

// Load reads the TOML config file from path, or the default path if empty.
func Load(customPath string) (*Config, string, error) {
	path := customPath
	if path == "" {
		path = constants.DefaultConfigPath()
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, ErrConfigNotFound
		}
		return nil, path, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, path, fmt.Errorf("failed to parse TOML config file %s: %w", path, err)
	}

	if err := cfg.ExpandPaths(); err != nil {
		return nil, path, err
	}

	return cfg, path, nil
}
