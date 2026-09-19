package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/AdmGenSameer/gmove/internal/config"
	"github.com/AdmGenSameer/gmove/internal/constants"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/deletion"
	"github.com/AdmGenSameer/gmove/internal/logger"
	"github.com/AdmGenSameer/gmove/internal/rclone"
	"github.com/AdmGenSameer/gmove/internal/safety"
	"github.com/AdmGenSameer/gmove/internal/scanner"
	"github.com/AdmGenSameer/gmove/internal/transfer"
	"github.com/AdmGenSameer/gmove/internal/tui"
	"github.com/AdmGenSameer/gmove/internal/utils"
	"github.com/AdmGenSameer/gmove/internal/verification"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	// Root flags
	var configPath string
	var profileName string
	var sourceOverride string
	var remoteOverride string
	var destOverride string
	var dryRun bool

	fs := flag.NewFlagSet("gmove", flag.ContinueOnError)
	fs.StringVar(&configPath, "config", "", "Custom path to config.toml")
	fs.StringVar(&profileName, "profile", "", "Select configured profile (e.g. 'shows', 'movies')")
	fs.StringVar(&sourceOverride, "source", "", "Override source directory")
	fs.StringVar(&remoteOverride, "remote", "", "Override rclone remote")
	fs.StringVar(&destOverride, "destination", "", "Override remote destination path")
	fs.BoolVar(&dryRun, "dry-run", false, "Simulate operation without copying or deleting")

	// Fast-path: Check for help or version before attempting to load config or start wizard
	for _, a := range args {
		if a == "help" || a == "-h" || a == "--help" {
			printUsage()
			return nil
		}
		if a == "version" || a == "-v" || a == "--version" || a == "-version" {
			fmt.Printf("GMOVE v%s - Safe Media Migration Manager\n", constants.Version)
			return nil
		}
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	subArgs := fs.Args()

	// Load configuration
	cfg, resolvedPath, err := config.Load(configPath)
	if err != nil {
		if errors.Is(err, config.ErrConfigNotFound) {
			fmt.Println("╭────────────────────────────────────────────────────────────╮")
			fmt.Println("│                         GMOVE                              │")
			fmt.Println("│              Safe Media Migration Manager                  │")
			fmt.Println("╰────────────────────────────────────────────────────────────╯")
			fmt.Printf("\nNo configuration found. Launching initial setup wizard...\n\n")

			newCfg, wizErr := config.RunWizard(resolvedPath)
			if wizErr != nil {
				return fmt.Errorf("configuration wizard aborted: %w", wizErr)
			}
			cfg = newCfg
			fmt.Printf("\nConfiguration successfully saved to %s\n\n", resolvedPath)
		} else {
			return err
		}
	}

	// Apply Profile if specified
	if profileName != "" {
		if err := cfg.ApplyProfile(profileName); err != nil {
			return err
		}
	}

	// Apply CLI overrides
	if sourceOverride != "" {
		cfg.Source = sourceOverride
	}
	if remoteOverride != "" {
		cfg.Remote = remoteOverride
	}
	if destOverride != "" {
		cfg.RemotePath = destOverride
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		return err
	}

	// Initialize database
	db, err := database.Open(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to open database at %s: %w", cfg.Database, err)
	}
	defer db.Close()

	repo := database.NewRepository(db)
	logger.Init(repo)
	defer logger.Close()
	logger.Infof("cli", "GMOVE v%s started (source: %s, remote: %s, dryRun: %v)", constants.Version, cfg.Source, cfg.RemoteDestination(), dryRun)

	rcloneClient := rclone.NewSubprocessClient("")

	// Initialize safety validator & deleter
	val, err := safety.NewValidator(cfg.Source)
	if err != nil {
		logger.Errorf("cli", "Safety validator initialization failed: %v", err)
		return fmt.Errorf("safety validation error on source dir: %w", err)
	}
	deleter := deletion.NewDeleter(repo, val, cfg.Source)

	// Check subcommand
	if len(subArgs) > 0 {
		subcommand := subArgs[0]
		subParams := subArgs[1:]

		switch subcommand {
		case "scan":
			return cmdScan(cfg)
		case "status":
			return cmdStatus(cfg, repo)
		case "history":
			return cmdHistory(repo, subParams)
		case "resume":
			return cmdResume(cfg, repo, rcloneClient, subParams)
		case "retry":
			return cmdRetry(cfg, repo, rcloneClient, subParams)
		case "verify":
			return cmdVerify(cfg, repo, rcloneClient, subParams)
		case "logs":
			return cmdLogs(repo, subParams)
		case "config":
			return cmdConfig(cfg, resolvedPath, rcloneClient, subParams)
		case "help", "--help", "-h":
			printUsage()
			return nil
		default:
			return fmt.Errorf("unknown subcommand: '%s'. Run 'gmove help' for usage", subcommand)
		}
	}

	// Default: Check rclone availability and launch interactive TUI
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := rcloneClient.CheckExecutable(ctx); err != nil {
		fmt.Println("╭────────────────────────────────────────────────────────────╮")
		fmt.Println("│                      ⚠ RCLONE MISSING                      │")
		fmt.Println("╰────────────────────────────────────────────────────────────╯")
		fmt.Printf("\nERROR: rclone was not found on your system PATH.\n\n")
		fmt.Println("GMOVE requires rclone to transfer data safely to Google Drive.")
		fmt.Println("Please install rclone and configure your remote before using GMOVE:")
		fmt.Printf("  https://rclone.org/downloads/\n\n")
		return err
	}

	// Launch Bubble Tea TUI
	model := tui.NewModel(cfg, repo, rcloneClient, deleter)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func printUsage() {
	fmt.Println(`GMOVE — Safe Media Migration Manager

Usage:
  gmove [flags] [subcommand]

Subcommands:
  scan                   Scan local media library and show available items
  status                 Show local vs remote status and last migration result
  history [id]           List migration history or inspect a specific operation
  resume [id]            Resume an interrupted or incomplete migration
  retry [id]             Retry failed files from an operation
  verify [id]            Re-verify transferred files against Google Drive
  logs [flags]           Inspect SQLite audit logs and error history
  config [show|check]    View configuration or test rclone connectivity
  version                Print GMOVE version
  help                   Show this help message

Flags:
  --profile NAME         Use configured profile (e.g. 'shows' or 'movies')
  --source PATH          Override local source media directory
  --remote NAME          Override rclone remote name (e.g. gdrive)
  --destination PATH     Override remote destination folder path
  --config PATH          Path to custom config.toml
  --dry-run              Simulate operations without transferring or deleting`)
}

func cmdScan(cfg *config.Config) error {
	fmt.Printf("Scanning source directory: %s ...\n\n", cfg.Source)
	s := scanner.New(cfg.Source)
	if len(cfg.IgnoreDirs) > 0 {
		s.SetIgnoreDirs(cfg.IgnoreDirs)
	}
	items, err := s.Scan()
	if err != nil {
		return err
	}

	if len(items) == 0 {
		fmt.Println("No media items found in source directory.")
		return nil
	}

	var totalBytes int64
	var totalFiles int

	fmt.Printf(" %-4s %-45s %-12s %-10s\n", "#", "Name", "Type", "Size")
	fmt.Println(strings.Repeat("─", 75))

	for i, item := range items {
		totalBytes += item.SizeBytes
		totalFiles += item.FileCount

		itemType := "File"
		if item.IsDirectory {
			itemType = fmt.Sprintf("Dir (%d files)", item.FileCount)
		}

		name := item.Name
		if len(name) > 43 {
			name = name[:40] + "..."
		}

		fmt.Printf(" %-4d %-45s %-12s %-10s\n", i+1, name, itemType, utils.FormatBytes(item.SizeBytes))
	}

	fmt.Println(strings.Repeat("─", 75))
	fmt.Printf("Total: %d media items (%d files), %s\n", len(items), totalFiles, utils.FormatBytes(totalBytes))

	disk, err := utils.GetDiskSpace(cfg.Source)
	if err == nil {
		fmt.Printf("Local filesystem: %s free / %s total\n",
			utils.FormatBytes(int64(disk.FreeBytes)),
			utils.FormatBytes(int64(disk.TotalBytes)),
		)
	}

	return nil
}

func cmdStatus(cfg *config.Config, repo *database.Repository) error {
	fmt.Println("╭────────────────────────────────────────────────────────────╮")
	fmt.Println("│                        GMOVE STATUS                        │")
	fmt.Println("╰────────────────────────────────────────────────────────────╯")
	fmt.Println()

	fmt.Printf("Source:      %s\n", cfg.Source)
	fmt.Printf("Destination: %s\n\n", cfg.RemoteDestination())

	disk, err := utils.GetDiskSpace(cfg.Source)
	if err == nil {
		fmt.Printf("Local storage:\n  Used: %s\n  Free: %s\n  Total: %s\n\n",
			utils.FormatBytes(int64(disk.UsedBytes)),
			utils.FormatBytes(int64(disk.FreeBytes)),
			utils.FormatBytes(int64(disk.TotalBytes)),
		)
	}

	// Query last operation
	lastOp, err := repo.GetLatestOperation()
	if err != nil {
		return err
	}
	if lastOp == nil {
		fmt.Println("No past migration operations recorded.")
		return nil
	}

	fmt.Printf("Last Operation: #%d\n", lastOp.ID)
	fmt.Printf("  Started:   %s\n", lastOp.StartedAt.Format(time.RFC822))
	fmt.Printf("  Status:    %s\n", lastOp.Status)
	fmt.Printf("  Transferred: %d files, %s\n", lastOp.TotalFiles, utils.FormatBytes(lastOp.TotalBytes))

	return nil
}

func cmdHistory(repo *database.Repository, args []string) error {
	if len(args) > 0 {
		opID, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid operation ID: %s", args[0])
		}

		op, err := repo.GetOperation(opID)
		if err != nil {
			return err
		}

		items, err := repo.GetTransferItems(opID)
		if err != nil {
			return err
		}

		fmt.Printf("\nOPERATION #%d\n", op.ID)
		fmt.Println(strings.Repeat("─", 65))
		fmt.Printf("Started:     %s\n", op.StartedAt.Format(time.RFC822))
		fmt.Printf("Status:      %s\n", op.Status)
		fmt.Printf("Source:      %s\n", op.Source)
		fmt.Printf("Destination: %s\n", op.Destination)
		fmt.Printf("Total:       %d items, %s\n\n", op.TotalItems, utils.FormatBytes(op.TotalBytes))

		fmt.Println("FILES:")
		fmt.Printf(" %-4s %-38s %-12s %-10s\n", "#", "Path", "Status", "Size")
		fmt.Println(strings.Repeat("─", 65))
		for i, it := range items {
			name := it.RelativePath
			if len(name) > 36 {
				name = name[:33] + "..."
			}
			fmt.Printf(" %-4d %-38s %-12s %-10s\n", i+1, name, it.Status, utils.FormatBytes(it.SizeBytes))
		}
		return nil
	}

	ops, err := repo.ListOperations(15)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		fmt.Println("No operations found in history.")
		return nil
	}

	fmt.Println("\nMIGRATION HISTORY")
	fmt.Printf(" %-6s %-18s %-8s %-12s %-15s\n", "ID", "Date", "Files", "Size", "Status")
	fmt.Println(strings.Repeat("─", 65))

	for _, op := range ops {
		dateStr := op.StartedAt.Format("02 Jan 2006")
		fmt.Printf(" #%-5d %-18s %-8d %-12s %-15s\n",
			op.ID, dateStr, op.TotalFiles, utils.FormatBytes(op.TotalBytes), op.Status)
	}

	fmt.Println("\nTo inspect an operation: gmove history <ID>")
	return nil
}

func cmdResume(cfg *config.Config, repo *database.Repository, client rclone.RcloneClient, args []string) error {
	var opID int64
	if len(args) > 0 {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid operation ID: %s", args[0])
		}
		opID = id
	} else {
		inc, err := repo.GetIncompleteOperation()
		if err != nil {
			return err
		}
		if inc == nil {
			fmt.Println("No incomplete or interrupted operation found to resume.")
			return nil
		}
		opID = inc.ID
	}

	op, err := repo.GetOperation(opID)
	if err != nil {
		return err
	}

	fmt.Printf("Resuming operation #%d (%s)...\n", op.ID, op.Destination)
	mgr := transfer.NewManager(cfg, repo, client)
	return mgr.Execute(context.Background(), op.ID, func(ev transfer.TransferEvent) {
		if ev.Item != nil {
			fmt.Printf("[%s] %s\n", ev.Type, ev.Item.Name)
		}
	})
}

func cmdRetry(cfg *config.Config, repo *database.Repository, client rclone.RcloneClient, args []string) error {
	var opID int64
	if len(args) > 0 {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid operation ID: %s", args[0])
		}
		opID = id
	} else {
		last, err := repo.GetLatestOperation()
		if err != nil || last == nil {
			return fmt.Errorf("no operation found to retry")
		}
		opID = last.ID
	}

	fmt.Printf("Retrying failed items from operation #%d...\n", opID)
	mgr := transfer.NewManager(cfg, repo, client)
	return mgr.RetryFailed(context.Background(), opID, func(ev transfer.TransferEvent) {
		if ev.Item != nil {
			fmt.Printf("[%s] %s\n", ev.Type, ev.Item.Name)
		}
	})
}

func cmdVerify(cfg *config.Config, repo *database.Repository, client rclone.RcloneClient, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: gmove verify <OPERATION_ID>")
	}
	opID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid operation ID: %s", args[0])
	}

	items, err := repo.GetTransferItems(opID)
	if err != nil {
		return err
	}

	fmt.Printf("Verifying %d items for operation #%d against %s...\n\n", len(items), opID, cfg.RemoteDestination())
	verifier := verification.NewVerifier(client, repo)

	for _, item := range items {
		fmt.Printf("Checking: %-40s ", item.RelativePath)
		err := verifier.VerifyItem(context.Background(), item, cfg.RemoteDestination())
		if err != nil {
			fmt.Printf("✗ FAILED (%v)\n", err)
		} else {
			fmt.Println("✓ VERIFIED")
		}
	}

	return nil
}

func cmdConfig(cfg *config.Config, path string, client rclone.RcloneClient, args []string) error {
	action := "show"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "check":
		fmt.Println("Checking rclone configuration and remote connectivity...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		ver, err := client.CheckExecutable(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("✓ %s\n", ver)

		remotes, err := client.ListRemotes(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("✓ Remotes available: %s\n", strings.Join(remotes, ", "))

		targetRemote := strings.TrimSuffix(cfg.Remote, ":")
		found := false
		for _, r := range remotes {
			if r == targetRemote {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("configured remote '%s' was not found in rclone listremotes", targetRemote)
		}
		fmt.Printf("✓ Configured remote '%s' is valid and accessible.\n", targetRemote)

	default: // show
		fmt.Println("GMOVE CONFIGURATION")
		fmt.Printf("Config file: %s\n\n", path)
		fmt.Printf("source                        = \"%s\"\n", cfg.Source)
		fmt.Printf("remote                        = \"%s\"\n", cfg.Remote)
		fmt.Printf("remote_path                   = \"%s\"\n", cfg.RemotePath)
		fmt.Printf("database                      = \"%s\"\n", cfg.Database)
		fmt.Printf("log_file                      = \"%s\"\n", cfg.LogFile)
		fmt.Printf("minimum_free_space_warning_gb = %d\n", cfg.MinimumFreeSpaceWarningGB)
		fmt.Printf("transfers                     = %d\n", cfg.Transfers)
		fmt.Printf("checkers                      = %d\n", cfg.Checkers)
	}

	return nil
}

func cmdLogs(repo *database.Repository, args []string) error {
	var opID int64
	var level string
	var component string
	var onlyErrors bool
	var limit int

	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.Int64Var(&opID, "op", 0, "Filter logs by operation ID")
	fs.StringVar(&level, "level", "", "Filter logs by level (DEBUG, INFO, WARN, ERROR)")
	fs.StringVar(&component, "component", "", "Filter logs by component (rclone, scanner, safety, transfer, etc.)")
	fs.BoolVar(&onlyErrors, "errors", false, "Show only WARN and ERROR events")
	fs.IntVar(&limit, "limit", 50, "Maximum number of logs to display")

	if err := fs.Parse(args); err != nil {
		return err
	}

	logger.Flush()

	filter := database.EventFilter{
		Level:      level,
		Component:  component,
		OnlyErrors: onlyErrors,
		Limit:      limit,
	}
	if opID > 0 {
		filter.OperationID = &opID
	}

	records, err := repo.QueryEvents(filter)
	if err != nil {
		return fmt.Errorf("failed to query logs from sqlite: %w", err)
	}

	fmt.Println("╭────────────────────────────────────────────────────────────╮")
	fmt.Println("│                         GMOVE LOGS                         │")
	fmt.Println("╰────────────────────────────────────────────────────────────╯")
	fmt.Println()

	if len(records) == 0 {
		fmt.Println("No log records found matching query criteria.")
		return nil
	}

	fmt.Printf("Displaying %d log events (newest first):\n\n", len(records))
	for _, rec := range records {
		ts := rec.Timestamp.Local().Format("2006-01-02 15:04:05")
		opStr := "        "
		if rec.OperationID != nil {
			opStr = fmt.Sprintf("op#%-5d", *rec.OperationID)
		}

		compStr := fmt.Sprintf("[%-8s]", rec.Component)

		var lvlStr string
		switch rec.Level {
		case "ERROR":
			lvlStr = "✗ ERROR"
		case "WARN":
			lvlStr = "⚠ WARN "
		case "INFO":
			lvlStr = "ℹ INFO "
		case "DEBUG":
			lvlStr = "• DEBUG"
		default:
			lvlStr = fmt.Sprintf("%-7s", rec.Level)
		}

		fmt.Printf("%s  %s  %s %s  %s\n", ts, lvlStr, compStr, opStr, rec.Message)
		if rec.Details != "" {
			fmt.Printf("    ↳ %s\n", rec.Details)
		}
	}
	fmt.Println()
	return nil
}
