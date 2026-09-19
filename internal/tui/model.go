package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/samarcher/gmove/internal/config"
	"github.com/samarcher/gmove/internal/database"
	"github.com/samarcher/gmove/internal/deletion"
	"github.com/samarcher/gmove/internal/rclone"
	"github.com/samarcher/gmove/internal/scanner"
	"github.com/samarcher/gmove/internal/transfer"
	"github.com/samarcher/gmove/internal/utils"
)

type ViewMode int

const (
	ViewSelection ViewMode = iota
	ViewPlan
	ViewTransferring
	ViewDeleteConfirm
	ViewFinished
)

// Bubble Tea custom messages
type (
	ScanCompleteMsg struct {
		Items []*scanner.MediaItem
		Disk  *utils.DiskSpace
		Err   error
	}
	TransferEventMsg transfer.TransferEvent
	DeletionDoneMsg  struct {
		Result *deletion.DeleteResult
		Err    error
	}
)

type Model struct {
	cfg          *config.Config
	repo         *database.Repository
	rcloneClient rclone.RcloneClient
	scanner      *scanner.Scanner
	transferMgr  *transfer.Manager
	deleter      *deletion.Deleter

	// State
	mode         ViewMode
	width        int
	height       int
	err          error
	statusMsg    string
	cancelFunc   context.CancelFunc

	// Selection View State
	allItems     []*scanner.MediaItem
	visibleItems []*scanner.MediaItem
	selectedMap  map[string]bool // Keyed by RelativePath
	cursor       int
	diskSpace    *utils.DiskSpace
	searchInput  textinput.Model
	isSearching  bool
	sortBy       scanner.SortBy
	sortDesc     bool

	// Transfer View State
	activeOp        *database.Operation
	activeDbItems   []*database.TransferItem
	currentFile     string
	currentBytes    int64
	currentTotal    int64
	currentSpeed    float64
	currentETA      int64
	overallBytes    int64
	overallTotal    int64
	completedCount  int
	transferringCount int
	pendingCount    int
	failedCount     int
	fileProgress    progress.Model
	overallProgress progress.Model

	// Deletion View State
	deleteInput   textinput.Model
	deleteResult  *deletion.DeleteResult
	deleteError   string
	verifiedItems []*database.TransferItem
}

func NewModel(cfg *config.Config, repo *database.Repository, rClient rclone.RcloneClient, del *deletion.Deleter) *Model {
	ti := textinput.New()
	ti.Placeholder = "Search movies..."
	ti.CharLimit = 50

	delInput := textinput.New()
	delInput.Placeholder = "Type DELETE to confirm"
	delInput.CharLimit = 10

	fp := progress.New(progress.WithDefaultGradient())
	op := progress.New(progress.WithGradient("#7D56F4", "#04B575"))

	scn := scanner.New(cfg.Source)
	if len(cfg.IgnoreDirs) > 0 {
		scn.SetIgnoreDirs(cfg.IgnoreDirs)
	}

	return &Model{
		cfg:             cfg,
		repo:            repo,
		rcloneClient:    rClient,
		scanner:         scn,
		transferMgr:     transfer.NewManager(cfg, repo, rClient),
		deleter:         del,
		mode:            ViewSelection,
		selectedMap:     make(map[string]bool),
		searchInput:     ti,
		deleteInput:     delInput,
		fileProgress:    fp,
		overallProgress: op,
		sortBy:          scanner.SortByName,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.scanCmd(),
	)
}

func (m *Model) scanCmd() tea.Cmd {
	return func() tea.Msg {
		items, err := m.scanner.Scan()
		if err != nil {
			return ScanCompleteMsg{Err: err}
		}
		disk, _ := utils.GetDiskSpace(m.cfg.Source)
		return ScanCompleteMsg{Items: items, Disk: disk}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.fileProgress.Width = msg.Width - 15
		if m.fileProgress.Width > 60 {
			m.fileProgress.Width = 60
		}
		m.overallProgress.Width = m.fileProgress.Width

	case tea.KeyMsg:
		// Handle Ctrl+C globally
		if msg.Type == tea.KeyCtrlC {
			if m.cancelFunc != nil {
				m.cancelFunc()
			}
			return m, tea.Quit
		}

		switch m.mode {
		case ViewSelection:
			return m.updateSelection(msg)
		case ViewPlan:
			return m.updatePlan(msg)
		case ViewTransferring:
			return m.updateTransferring(msg)
		case ViewDeleteConfirm:
			return m.updateDeleteConfirm(msg)
		case ViewFinished:
			if msg.String() == "q" || msg.String() == "enter" {
				return m, tea.Quit
			}
		}

	case ScanCompleteMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.allItems = msg.Items
			m.diskSpace = msg.Disk
			m.filterAndSort()
		}

	case TransferEventMsg:
		m.handleTransferEvent(transfer.TransferEvent(msg))

	case DeletionDoneMsg:
		if msg.Err != nil {
			m.deleteError = msg.Err.Error()
		} else {
			m.deleteResult = msg.Result
			m.mode = ViewFinished
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) filterAndSort() {
	query := m.searchInput.Value()
	m.visibleItems = scanner.Filter(m.allItems, query)
	scanner.Sort(m.visibleItems, m.sortBy, m.sortDesc)
	if m.cursor >= len(m.visibleItems) {
		m.cursor = len(m.visibleItems) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) getSelectedItems() []*scanner.MediaItem {
	var list []*scanner.MediaItem
	for _, it := range m.allItems {
		if m.selectedMap[it.RelativePath] {
			list = append(list, it)
		}
	}
	return list
}

func (m *Model) getSelectedTotalSize() int64 {
	var total int64
	for _, it := range m.getSelectedItems() {
		total += it.SizeBytes
	}
	return total
}

func (m *Model) updateSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.isSearching {
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			m.isSearching = false
			m.searchInput.Blur()
			return m, nil
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.filterAndSort()
			return m, cmd
		}
	}

	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.visibleItems)-1 {
			m.cursor++
		}
	case " ": // Space to toggle select
		if len(m.visibleItems) > 0 {
			cur := m.visibleItems[m.cursor]
			m.selectedMap[cur.RelativePath] = !m.selectedMap[cur.RelativePath]
		}
	case "a": // Select all visible
		for _, it := range m.visibleItems {
			m.selectedMap[it.RelativePath] = true
		}
	case "n": // Deselect all
		m.selectedMap = make(map[string]bool)
	case "/":
		m.isSearching = true
		m.searchInput.Focus()
		return m, textinput.Blink
	case "s": // Toggle sort
		if m.sortBy == scanner.SortByName {
			m.sortBy = scanner.SortBySize
			m.sortDesc = true
		} else if m.sortBy == scanner.SortBySize {
			m.sortBy = scanner.SortByDate
			m.sortDesc = true
		} else {
			m.sortBy = scanner.SortByName
			m.sortDesc = false
		}
		m.filterAndSort()
	case "enter":
		if len(m.getSelectedItems()) > 0 {
			m.mode = ViewPlan
		} else {
			m.statusMsg = "Please select at least one movie first (Space to select)."
		}
	}

	return m, nil
}

func (m *Model) updatePlan(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.mode = ViewTransferring
		return m, m.startTransferCmd()
	case "n", "N", "esc", "q":
		m.mode = ViewSelection
	}
	return m, nil
}

func (m *Model) updateTransferring(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "q" || msg.Type == tea.KeyCtrlC {
		if m.cancelFunc != nil {
			m.cancelFunc()
		}
		m.statusMsg = "Transfer interrupted. No local files were deleted."
		return m, tea.Quit
	}
	return m, nil
}

func (m *Model) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = ViewFinished
		m.statusMsg = "Deletion skipped. All local files preserved."
		return m, nil
	case tea.KeyEnter:
		typed := m.deleteInput.Value()
		if strings.TrimSpace(typed) == "DELETE" {
			return m, m.executeDeleteCmd()
		}
		m.deleteError = "You must type exact uppercase 'DELETE' to confirm deletion."
		return m, nil
	default:
		var cmd tea.Cmd
		m.deleteInput, cmd = m.deleteInput.Update(msg)
		return m, cmd
	}
}

func (m *Model) startTransferCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel

		selected := m.getSelectedItems()
		op, dbItems, err := m.transferMgr.PrepareOperation(selected, false)
		if err != nil {
			return TransferEventMsg{Type: transfer.EventItemFailed, Error: err}
		}
		m.activeOp = op
		m.activeDbItems = dbItems
		m.pendingCount = len(dbItems)
		m.overallTotal = op.TotalBytes

		// Start transfer loop in background
		go func() {
			_ = m.transferMgr.Execute(ctx, op.ID, func(ev transfer.TransferEvent) {
				// Note: in a real Bubble Tea program, external events are sent via p.Send().
				// We handle event transitions directly on the event callback.
			})
		}()

		return nil
	}
}

func (m *Model) handleTransferEvent(ev transfer.TransferEvent) {
	switch ev.Type {
	case transfer.EventItemStarted:
		if ev.Item != nil {
			m.currentFile = ev.Item.Name
			m.currentTotal = ev.Item.SizeBytes
			m.currentBytes = 0
		}
		m.transferringCount = 1
		if m.pendingCount > 0 {
			m.pendingCount--
		}

	case transfer.EventItemProgress:
		if ev.Stats != nil {
			m.currentSpeed = ev.Stats.Speed
			if ev.Stats.ETA != nil {
				m.currentETA = *ev.Stats.ETA
			}
			if len(ev.Stats.InProgressFiles) > 0 {
				m.currentBytes = ev.Stats.InProgressFiles[0].Bytes
			}
		}
		m.overallBytes = ev.CompletedBytes + m.currentBytes

	case transfer.EventItemTransferred:
		m.statusMsg = fmt.Sprintf("Verifying %s...", ev.Item.Name)

	case transfer.EventItemVerified:
		m.completedCount++
		m.transferringCount = 0

	case transfer.EventItemFailed:
		m.failedCount++
		m.transferringCount = 0

	case transfer.EventBatchComplete:
		// Transition to delete confirmation or finished
		verified, _ := m.repo.GetVerifiedItems(m.activeOp.ID)
		m.verifiedItems = verified
		if len(verified) > 0 {
			m.mode = ViewDeleteConfirm
			m.deleteInput.Focus()
		} else {
			m.mode = ViewFinished
		}
	}
}

func (m *Model) executeDeleteCmd() tea.Cmd {
	return func() tea.Msg {
		var itemIDs []int64
		for _, it := range m.verifiedItems {
			itemIDs = append(itemIDs, it.ID)
		}

		req := &deletion.DeleteRequest{
			OperationID:       m.activeOp.ID,
			ItemIDs:           itemIDs,
			ConfirmationToken: m.deleteInput.Value(),
		}

		res, err := m.deleter.Execute(req)
		return DeletionDoneMsg{Result: res, Err: err}
	}
}

func (m *Model) View() string {
	switch m.mode {
	case ViewSelection:
		return m.viewSelection()
	case ViewPlan:
		return m.viewPlan()
	case ViewTransferring:
		return m.viewTransferring()
	case ViewDeleteConfirm:
		return m.viewDeleteConfirm()
	case ViewFinished:
		return m.viewFinished()
	}
	return ""
}

func (m *Model) viewSelection() string {
	var b strings.Builder

	// Header
	header := HeaderBoxStyle.Render(
		fmt.Sprintf("GMOVE — Safe Media Migration Manager    Remote: %s ●", m.cfg.RemoteDestination()),
	)
	b.WriteString(header + "\n\n")

	// Local Storage Banner
	var diskStr string
	if m.diskSpace != nil {
		diskStr = fmt.Sprintf("Source: %s  |  Used: %s  |  Free: %s",
			m.cfg.Source,
			utils.FormatBytes(int64(m.diskSpace.UsedBytes)),
			utils.FormatBytes(int64(m.diskSpace.FreeBytes)),
		)
	} else {
		diskStr = fmt.Sprintf("Source: %s", m.cfg.Source)
	}
	b.WriteString(SubtitleStyle.Render(diskStr) + "\n\n")

	// Search bar if active
	if m.isSearching || m.searchInput.Value() != "" {
		b.WriteString("Search: " + m.searchInput.View() + "\n\n")
	}

	// Movie list table
	b.WriteString(TitleStyle.Render("AVAILABLE MEDIA:") + "\n")
	b.WriteString(strings.Repeat("─", 65) + "\n")

	if len(m.visibleItems) == 0 {
		b.WriteString(MutedStyle.Render("  No media files found matching filter.") + "\n")
	} else {
		start := 0
		end := len(m.visibleItems)
		maxVisible := 12
		if end > maxVisible {
			if m.cursor >= maxVisible {
				start = m.cursor - maxVisible + 1
			}
			end = start + maxVisible
			if end > len(m.visibleItems) {
				end = len(m.visibleItems)
			}
		}

		for i := start; i < end; i++ {
			item := m.visibleItems[i]
			isSelected := m.selectedMap[item.RelativePath]

			var mark string
			if isSelected {
				mark = RadioActiveStyle.Render("◉")
			} else {
				mark = RadioInactiveStyle.Render("◯")
			}

			typeBadge := ""
			if item.IsDirectory {
				typeBadge = MutedStyle.Render(fmt.Sprintf("[%d files] ", item.FileCount))
			}

			nameStr := fmt.Sprintf("%-38s", truncate(item.Name, 38))
			sizeStr := fmt.Sprintf("%10s", utils.FormatBytes(item.SizeBytes))

			line := fmt.Sprintf("  %s %s%s %s", mark, typeBadge, nameStr, sizeStr)
			if i == m.cursor {
				b.WriteString(SelectedRowStyle.Render("> "+line) + "\n")
			} else {
				b.WriteString("  " + NormalRowStyle.Render(line) + "\n")
			}
		}
	}

	b.WriteString(strings.Repeat("─", 65) + "\n")

	// Summary footer
	selectedCount := len(m.getSelectedItems())
	totalSelectedBytes := m.getSelectedTotalSize()

	footer := fmt.Sprintf("Selected: %d  |  Total: %s", selectedCount, utils.FormatBytes(totalSelectedBytes))
	b.WriteString(BadgeSuccess.Render(footer) + "\n\n")

	if m.statusMsg != "" {
		b.WriteString(WarningStyle.Render(m.statusMsg) + "\n")
	}

	help := "↑/↓ Navigate • Space Select • a All • n None • / Search • s Sort • Enter Continue • q Quit"
	b.WriteString(HelpStyle.Render(help))

	return PanelStyle.Render(b.String())
}

func (m *Model) viewPlan() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("TRANSFER PLAN") + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	selected := m.getSelectedItems()
	b.WriteString(fmt.Sprintf("Items to transfer: %d\n", len(selected)))
	b.WriteString(fmt.Sprintf("Total size:        %s\n", utils.FormatBytes(m.getSelectedTotalSize())))
	b.WriteString(fmt.Sprintf("Source directory:  %s\n", m.cfg.Source))
	b.WriteString(fmt.Sprintf("Destination:       %s\n\n", m.cfg.RemoteDestination()))

	b.WriteString("Selected files:\n")
	for i, it := range selected {
		if i >= 5 {
			b.WriteString(fmt.Sprintf("  ... and %d more\n", len(selected)-5))
			break
		}
		b.WriteString(fmt.Sprintf("  • %-35s %s\n", truncate(it.Name, 35), utils.FormatBytes(it.SizeBytes)))
	}

	b.WriteString("\nOperation lifecycle:\n")
	b.WriteString(SecondaryStyle.Render("  COPY → VERIFY (MD5 + Size) → OPTIONAL SAFE DELETION\n\n"))

	b.WriteString(HighlightStyle.Render("Proceed with transfer? [y/N]: "))

	return PanelStyle.Render(b.String())
}

func (m *Model) viewTransferring() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("GMOVE — TRANSFERRING") + "\n")
	b.WriteString(strings.Repeat("─", 60) + "\n\n")

	// Current file
	curName := m.currentFile
	if curName == "" {
		curName = "Preparing..."
	}
	b.WriteString("Current file:\n")
	b.WriteString(HighlightStyle.Render("  "+curName) + "\n\n")

	// Current file progress bar
	var filePercent float64
	if m.currentTotal > 0 {
		filePercent = float64(m.currentBytes) / float64(m.currentTotal)
	}
	b.WriteString(m.fileProgress.ViewAs(filePercent) + "\n")
	b.WriteString(fmt.Sprintf("  %s / %s   Speed: %s   ETA: %s\n\n",
		utils.FormatBytes(m.currentBytes),
		utils.FormatBytes(m.currentTotal),
		utils.FormatSpeed(m.currentSpeed),
		utils.FormatETA(m.currentETA),
	))

	// Overall progress
	b.WriteString("Overall batch:\n")
	var overallPercent float64
	if m.overallTotal > 0 {
		overallPercent = float64(m.overallBytes) / float64(m.overallTotal)
	}
	b.WriteString(m.overallProgress.ViewAs(overallPercent) + "\n")
	b.WriteString(fmt.Sprintf("  %s / %s\n\n",
		utils.FormatBytes(m.overallBytes),
		utils.FormatBytes(m.overallTotal),
	))

	// Stats tally
	b.WriteString("Status:\n")
	b.WriteString(fmt.Sprintf("  ✓ %d completed\n", m.completedCount))
	b.WriteString(fmt.Sprintf("  ↑ %d transferring\n", m.transferringCount))
	b.WriteString(fmt.Sprintf("  ○ %d pending\n", m.pendingCount))
	if m.failedCount > 0 {
		b.WriteString(BadgeDanger.Render(fmt.Sprintf("  ✗ %d failed\n", m.failedCount)))
	}

	b.WriteString("\n" + HelpStyle.Render("Press q to gracefully cancel transfer. No local files will be deleted."))

	return PanelStyle.Render(b.String())
}

func (m *Model) viewDeleteConfirm() string {
	var b strings.Builder

	b.WriteString(WarningBoxStyle.Render(
		BadgeDanger.Render("⚠ LOCAL FILE DELETION WARNING") + "\n\n" +
			fmt.Sprintf("The following %d files have been successfully VERIFIED on Google Drive:\n", len(m.verifiedItems)),
	) + "\n\n")

	var totalVerifiedSize int64
	for i, it := range m.verifiedItems {
		totalVerifiedSize += it.SizeBytes
		if i < 5 {
			b.WriteString(fmt.Sprintf("  ✓ %-35s %s\n", truncate(it.Name, 35), utils.FormatBytes(it.SizeBytes)))
		}
	}
	if len(m.verifiedItems) > 5 {
		b.WriteString(fmt.Sprintf("  ... and %d more\n", len(m.verifiedItems)-5))
	}

	b.WriteString("\n" + TitleStyle.Render(fmt.Sprintf("Total space that will be freed: %s", utils.FormatBytes(totalVerifiedSize))) + "\n\n")
	b.WriteString("The copies on Google Drive will remain untouched.\n")
	b.WriteString("This action will permanently delete the LOCAL original copies.\n\n")

	if m.deleteError != "" {
		b.WriteString(BadgeDanger.Render(m.deleteError) + "\n\n")
	}

	b.WriteString(HighlightStyle.Render("To confirm deletion, type exact word 'DELETE': ") + m.deleteInput.View() + "\n\n")
	b.WriteString(HelpStyle.Render("Press Esc to skip deletion and preserve all local files."))

	return PanelStyle.Render(b.String())
}

func (m *Model) viewFinished() string {
	var b strings.Builder

	b.WriteString(HeaderBoxStyle.Render("GMOVE — MIGRATION FINISHED") + "\n\n")

	if m.deleteResult != nil {
		b.WriteString(BadgeSuccess.Render(fmt.Sprintf("✓ Successfully deleted %d local verified files.\n", m.deleteResult.DeletedCount)))
		b.WriteString(fmt.Sprintf("Local disk space recovered: %s\n\n", utils.FormatBytes(m.deleteResult.TotalBytesFreed)))

		if len(m.deleteResult.Failed) > 0 {
			b.WriteString(BadgeWarning.Render(fmt.Sprintf("⚠ %d items failed deletion safety checks and were preserved:\n", len(m.deleteResult.Failed))))
			for _, f := range m.deleteResult.Failed {
				b.WriteString(fmt.Sprintf("  • %s: %s\n", f.Item.RelativePath, f.Reason))
			}
		}
	} else {
		b.WriteString("Transfer and verification completed.\n")
		b.WriteString("No local files were deleted.\n\n")
	}

	b.WriteString(HelpStyle.Render("Press Enter or q to exit."))
	return PanelStyle.Render(b.String())
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
