package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/AdmGenSameer/gmove/internal/constants"
)

// RunWizard prompts the user interactively for source dir, remote name, and remote path.
func RunWizard(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = constants.DefaultConfigPath()
	}

	cfg := DefaultConfig()

	var sourceDir string
	var remoteName string
	var remotePath string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Local Media Directory").
				Description("Absolute path to your local media/movies directory on this server").
				Placeholder("/mnt/hdd/Movies").
				Value(&sourceDir).
				Validate(func(str string) error {
					s := strings.TrimSpace(str)
					if s == "" {
						return fmt.Errorf("source directory cannot be empty")
					}
					if !filepath.IsAbs(s) {
						return fmt.Errorf("path must be absolute (e.g. /mnt/hdd/Movies)")
					}
					return nil
				}),

			huh.NewInput().
				Title("rclone Remote Name").
				Description("Name of your configured rclone remote (e.g. 'gdrive')").
				Placeholder("gdrive").
				Value(&remoteName).
				Validate(func(str string) error {
					if strings.TrimSpace(str) == "" {
						return fmt.Errorf("remote name cannot be empty")
					}
					return nil
				}),

			huh.NewInput().
				Title("Remote Directory Path").
				Description("Destination folder path inside the remote (e.g. 'Movies')").
				Placeholder("Movies").
				Value(&remotePath),
		),
	)

	err := form.Run()
	if err != nil {
		return nil, fmt.Errorf("wizard cancelled: %w", err)
	}

	cfg.Source = strings.TrimSpace(sourceDir)
	cfg.Remote = strings.TrimSuffix(strings.TrimSpace(remoteName), ":")
	cfg.RemotePath = strings.TrimPrefix(strings.TrimSpace(remotePath), "/")

	// Create directories if source doesn't exist
	if _, err := os.Stat(cfg.Source); os.IsNotExist(err) {
		_ = os.MkdirAll(cfg.Source, 0755)
	}

	if err := cfg.Save(configPath); err != nil {
		return nil, fmt.Errorf("failed to save configuration: %w", err)
	}

	return cfg, nil
}
