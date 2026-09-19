# GMOVE

> **Safe Media Migration Manager**  
> *A production-quality Linux CLI & TUI tool for safely migrating large media files from local server storage to Google Drive using rclone.*

---

## The Problem GMOVE Solves

When managing a home server or NAS with large media libraries (20 GB–100 GB+ remuxes), moving files to cloud storage from a desktop file manager (like Dolphin, Nautilus, or macOS Finder over SFTP/SMB) inadvertently funnels the data transfer through your desktop PC:

```
[Server HDD] ──(SFTP/SMB)──> [Desktop PC] ──(SFTP/SMB)──> [Server rclone mount] ──> [Google Drive]
```

This creates severe network bottlenecks, double-hops your local LAN bandwidth, and can lead to silent corruptions or interrupted transfers if the client computer sleeps or reboots.

**GMOVE eliminates the client workstation from the data path**, executing the migration directly on the Linux server:

```
[Local Server HDD] ───────(GMOVE + rclone)───────> [Google Drive]
```

GMOVE is **NOT** a simple wrapper around `mv` or `rclone copy`. It is an engineered, safety-oriented migration orchestrator where **data loss prevention is the highest priority**.

---

## Core Safety Invariants

> **Golden Rule**: GMOVE never deletes a local source file simply because `rclone` reported an exit code of `0`.

### The Deletion Gate

The TUI has zero deletion authority. File deletion is mediated exclusively by `safety.Validator` and `deletion.Deleter`:

```
                       DELETE REQUEST
                             │
                             ▼
                   ┌───────────────────┐
                   │ Safety Validator  │
                   └─────────┬─────────┘
                             │
             ┌───────────────┼────────────────┐
             ▼               ▼                ▼
        DB status        Path safety      File unchanged
         VERIFIED        validated      size + mtime + inode
             │               │                │
             └───────────────┼────────────────┘
                             ▼
                     Exact token = DELETE?
                             │
                       ┌─────┴─────┐
                      NO           YES
                      │             │
                      ▼             ▼
                   REJECT      Deletion allowed
                                     │
                                     ▼
                                Delete file
                                     │
                                     ▼
                               Update SQLite
                                     │
                                     ▼
                                  DELETED
```

1. **Strict State Enforcement**: A file cannot enter `DELETE_PENDING` unless its status is verified (`VERIFIED`) in the SQLite database.
2. **Independent Verification**: Google Drive stores MD5 checksums for ingested files. GMOVE runs `rclone check --one-way` to ensure that both the remote byte count and MD5 match the source file before marking it `VERIFIED`.
3. **Path Confinement**: All paths are cleaned and canonically verified against the configured source directory (`filepath.Clean` and `filepath.Rel`). Source cannot be `/`, user home root, or empty. Symlinks escaping the source directory are strictly rejected.
4. **Liveness & Drift Detection**: Stored identity tracks `size`, `mtime`, and Linux `inode` (`syscall.Stat_t`). Immediately before deletion, the file is re-stated. If a torrent client or transcoder modified or replaced the file in the interim, deletion is aborted.
5. **Exact Authorization Token**: The confirmation prompt requires typing the exact uppercase string `DELETE`. `y`, `yes`, `Y`, or Enter are rejected.
6. **Partial Success Isolation**: If 9 files succeed and 1 fails, **NO files are deleted by default**. The user can choose to delete only the verified subset (which still requires the full `DELETE` confirmation).
7. **Crash & Signal Resilience**: `Ctrl+C` or `SIGTERM` gracefully stops the transfer subprocess and records state as `INTERRUPTED`. No local files are touched. Transfers can be resumed anytime with `gmove resume`.

---

## Tech Stack

| Component | Technology | Purpose |
| :--- | :--- | :--- |
| **Language** | **Go 1.27+** | Single static binary, low memory footprint, zero runtime dependencies |
| **TUI Engine** | **Bubble Tea** | Elm-architecture terminal UI for responsive, stateful interfaces |
| **Styling** | **Lip Gloss** | Terminal layouts, borders, and curated color palettes |
| **Widgets** | **Bubbles** | Multi-bar live progress displays and text inputs |
| **Forms** | **Huh** | First-run interactive setup wizard and warning confirmations |
| **Database** | **SQLite (modernc.org/sqlite)** | Pure-Go CGO-free driver in WAL mode for persistent operation state |
| **Storage Backend** | **rclone** | Native Google Drive authentication, streaming, and verification |
| **Configuration** | **TOML (pelletier/go-toml/v2)** | Clean, human-readable configuration conforming to XDG standards |

---

## Installation & Building

### Prerequisites
* Linux (x86_64 or ARM64)
* `rclone` installed and configured with your Google Drive remote (e.g. `gdrive:`)
* Go 1.21+ (to compile from source)

### Compiling from Source
```bash
git clone https://github.com/samarcher/gmove.git
cd gmove
make build
```

This compiles a single static binary: `./gmove`.

To install to `~/.local/bin/gmove`:
```bash
make install
```

---

## Configuration

GMOVE uses an XDG-compliant configuration file at `~/.config/gmove/config.toml`.

### First Run Wizard
On the first launch of `gmove`, if no configuration is found, an interactive wizard prompts you for:
1. **Local Media Directory** (e.g. `/mnt/hdd/Movies`)
2. **rclone Remote Name** (e.g. `gdrive`)
3. **Remote Destination Path** (e.g. `Movies`)

### Configuration Reference (`config.toml`)
```toml
source = "/mnt/hdd/Movies"
remote = "gdrive"
remote_path = "Movies"
database = "~/.local/share/gmove/gmove.db"
log_file = "~/.local/state/gmove/gmove.log"
minimum_free_space_warning_gb = 100
transfers = 4
checkers = 8
retries = 3
low_level_retries = 10
drive_chunk_size = "64M"
delete_after_verify = false
ignore_dirs = ["Cloudbackup", "Music", "incomplete", "prowlarr", "sonarr", "radarr"]

# Named profiles for different media categories:
[profiles.movies]
source = "/mnt/media/movies"
remote_path = "Movies"

[profiles.shows]
source = "/mnt/nextcloud-hdd/downloads/torrents/shows"
remote_path = "Shows"
```

### Switching Profiles
Run GMOVE for your TV shows library:
```bash
gmove --profile shows
```
Or for movies:
```bash
gmove --profile movies
```

You can view your active configuration at any time:
```bash
gmove config show
```

To test connectivity to your rclone remote:
```bash
gmove config check
```

---

## Terminal UI (TUI) Experience

Launch the interactive interface simply by running:
```bash
gmove
```

### Media Selection View
```
╭────────────────────────────────────────────────────────────╮
│ GMOVE — Safe Media Migration Manager    Remote: gdrive:Movies ● │
├────────────────────────────────────────────────────────────┤
│ Source: /mnt/hdd/Movies  |  Used: 4.7 TB  |  Free: 280 GB   │
│                                                            │
│ AVAILABLE MEDIA:                                           │
│ ─────────────────────────────────────────────────────────  │
│ > ◉ Interstellar (2014).mkv                      27.1 GB   │
│   ◯ Dune (2021) [4 files]                        18.4 GB   │
│   ◯ Oppenheimer (2023).mkv                       21.7 GB   │
│   ◯ Avatar (2022).mkv                            31.2 GB   │
│ ─────────────────────────────────────────────────────────  │
│ Selected: 1  |  Total: 27.1 GB                             │
│                                                            │
│ ↑/↓ Navigate • Space Select • a All • / Search • Enter OK  │
╰────────────────────────────────────────────────────────────╯
```

**Keybindings**:
* `↑` / `↓` or `k` / `j`: Navigate through list
* `Space`: Toggle item selection
* `a`: Select all visible items
* `n`: Deselect all items
* `/`: Search / filter media titles
* `s`: Cycle sort order (Name, Size, Modification Date)
* `Enter`: Proceed to transfer plan
* `q`: Exit

### Directory Bundles Support
If a movie is stored inside a dedicated folder (e.g. `Dune (2021)/` with `Dune.mkv`, `Dune.nfo`, `poster.jpg`, and `subtitles.srt`), GMOVE treats the folder as a single migration item, migrating all files with their relative structure intact.

---

## CLI Commands for Automation

GMOVE includes full CLI subcommands for non-interactive inspections, scripting, and recovery:

### Scan Media Library
```bash
gmove scan
```
Scans the local source directory and displays a formatted table of media files and directories, total byte counts, and filesystem free space.

### Status Overview
```bash
gmove status
```
Shows local storage stats, Google Drive target, and details of the latest migration operation.

### Operation History
```bash
# List recent operations
gmove history

# Inspect a specific operation in detail
gmove history 42
```

### Resuming Interrupted Operations
```bash
# Resume latest interrupted or incomplete operation
gmove resume

# Resume a specific operation
gmove resume 42
```
Filters out all already-verified items and resumes remaining items seamlessly.

### Retrying Failed Transfers
```bash
gmove retry [OPERATION_ID]
```
Retries only files marked `FAILED`, without re-transferring already verified files.

### Verifying Past Migrations
```bash
gmove verify 42
```
Performs a live post-transfer check against Google Drive for all files in operation #42.

### Dry Run Simulation
Add `--dry-run` to any command to simulate discovery, planning, and transfer without modifying local or remote storage:
```bash
gmove --dry-run
```

---

## Edge Cases Handled

* **Google Drive 750 GB/Day Upload Limit**: If Google Drive hits its daily account limit (`HTTP 403 userRateLimitExceeded`), GMOVE detects the quota error, pauses gracefully, records the operation as `PAUSED`, and avoids crashing.
* **Hardlink Detection**: Home server torrent setups often hardlink video files for seeding (`st_nlink > 1`). GMOVE inspects `st_nlink` and warns the user that deleting a hardlinked file will not free physical disk space until all links are unlinked.
* **Incomplete Downloads**: Temporary files (`.part`, `.!qB`, `.crdownload`, `.tmp`) are automatically ignored by the scanner.
* **Dangling Empty Directories**: When all verified files in a movie directory are deleted, GMOVE automatically cleans up the empty directory.

---

## Running Tests

Run the full test suite:
```bash
make test
```
Or directly with Go:
```bash
go test -v ./tests
```

---

## Architecture & Code Boundaries

```
gmove/
├── cmd/
│   └── gmove/
│       └── main.go         # CLI dispatch (TUI vs subcommands)
├── internal/
│   ├── config/             # TOML loader, validation, and Huh wizard
│   ├── constants/          # Status enums, file extensions, paths
│   ├── database/           # SQLite repository, WAL mode, migrations
│   ├── deletion/           # Gated Deleter (ONLY component allowed to unlink)
│   ├── rclone/             # Subprocess adapter with async JSON stats parser
│   ├── safety/             # Path confinement and identity drift validator
│   ├── scanner/            # Media discovery, folder grouping, range selection
│   ├── transfer/           # Transfer lifecycle manager
│   ├── tui/                # Bubble Tea model, Lip Gloss styles, progress views
│   ├── utils/              # Byte formatting, speed, ETA, and disk space
│   └── verification/       # rclone check and MD5 verification engine
├── tests/                  # Exhaustive unit and integration test suite
├── Makefile
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

---

## License

MIT License. See [LICENSE](file:///home/samarcher/Projects/gmove/LICENSE) for details.
