package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/AdmGenSameer/gmove/internal/config"
	"github.com/AdmGenSameer/gmove/internal/database"
	"github.com/AdmGenSameer/gmove/internal/deletion"
	"github.com/AdmGenSameer/gmove/internal/logger"
	"github.com/AdmGenSameer/gmove/internal/rclone"
	"github.com/AdmGenSameer/gmove/internal/scanner"
	"github.com/AdmGenSameer/gmove/internal/transfer"
	"github.com/AdmGenSameer/gmove/internal/utils"
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
	OperationPreparedMsg struct {
		Op      *database.Operation
		DbItems []*database.TransferItem
		Err     error
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
	activeOp          *database.Operation
	activeDbItems     []*database.TransferItem
	eventChan         chan transfer.TransferEvent
	logLines          []string
	currentFile       string
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

	fp := progress.New(progress.WithGradient("#8BE9FD", "#7D56F4"))
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
			logger.Errorf("tui", "Library scan failed: %v", msg.Err)
		} else {
			m.allItems = msg.Items
			m.diskSpace = msg.Disk
			logger.Infof("tui", "Scan finished: %d media items discovered", len(msg.Items))
			m.filterAndSort()
		}

	case OperationPreparedMsg:
		if msg.Err != nil {
			m.err = msg.Err
			m.statusMsg = fmt.Sprintf("Error preparing operation: %v", msg.Err)
			m.addLog(fmt.Sprintf("[ERROR] Prepare failed: %v", msg.Err))
			logger.Errorf("tui", "Operation preparation failed: %v", msg.Err)
			return m, nil
		}
		m.activeOp = msg.Op
		m.activeDbItems = msg.DbItems
		m.pendingCount = len(msg.DbItems)
		m.overallTotal = msg.Op.TotalBytes
		m.overallBytes = 0
		m.completedCount = 0
		m.failedCount = 0
		m.currentFile = "Starting transfer..."
		m.addLog(fmt.Sprintf("[DB] Operation #%d recorded (%d items, %s)", msg.Op.ID, len(msg.DbItems), utils.FormatBytes(msg.Op.TotalBytes)))
		m.addLog("[RCLONE] Connecting to remote destination...")
		logger.For("tui").WithOp(msg.Op.ID).Infof("Transfer screen active for operation #%d (%d items)", msg.Op.ID, len(msg.DbItems))

		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFunc = cancel
		m.eventChan = make(chan transfer.TransferEvent, 200)

		go func() {
			_ = m.transferMgr.Execute(ctx, msg.Op.ID, func(ev transfer.TransferEvent) {
				m.eventChan <- ev
			})
			close(m.eventChan)
		}()

		return m, m.waitForEventCmd()

	case TransferEventMsg:
		ev := transfer.TransferEvent(msg)
		m.handleTransferEvent(ev)
		if m.mode == ViewTransferring {
			return m, m.waitForEventCmd()
		}
		return m, nil

	case DeletionDoneMsg:
		if msg.Err != nil {
			m.deleteError = msg.Err.Error()
			logger.Errorf("tui", "Deletion failed: %v", msg.Err)
		} else {
			m.deleteResult = msg.Result
			m.mode = ViewFinished
			logger.Infof("tui", "Deletion completed: %d items deleted, %d bytes freed", msg.Result.DeletedCount, msg.Result.TotalBytesFreed)
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
		m.logLines = nil
		m.currentFile = "Preparing operation in database..."
		m.addLog("[PLAN] User confirmed transfer plan. Initializing operation...")
		return m, m.prepareOperationCmd()
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

func (m *Model) prepareOperationCmd() tea.Cmd {
	selected := m.getSelectedItems()
	return func() tea.Msg {
		op, dbItems, err := m.transferMgr.PrepareOperation(selected, false)
		return OperationPreparedMsg{Op: op, DbItems: dbItems, Err: err}
	}
}

func (m *Model) waitForEventCmd() tea.Cmd {
	return func() tea.Msg {
		if m.eventChan == nil {
			return nil
		}
		ev, ok := <-m.eventChan
		if !ok {
			return nil
		}
		return TransferEventMsg(ev)
	}
}

func (m *Model) addLog(msg string) {
	timestamp := time.Now().Format("15:04:05")
	line := fmt.Sprintf("%s %s", timestamp, msg)
	m.logLines = append(m.logLines, line)
	if len(m.logLines) > 100 {
		m.logLines = m.logLines[len(m.logLines)-100:]
	}
}

func (m *Model) handleTransferEvent(ev transfer.TransferEvent) {
	if ev.Message != "" {
		m.addLog(ev.Message)
	}

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
		m.transferringCount = 0
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

func formatLogLine(line string, maxLen int) string {
	if maxLen > 5 && len(line) > maxLen {
		line = line[:maxLen-3] + "..."
	}
	if strings.Contains(line, "[VERIFIED]") || strings.Contains(line, "[SUCCESS]") {
		return SecondaryStyle.Render(line)
	}
	if strings.Contains(line, "[START") || strings.Contains(line, "[TRANSFER]") || strings.Contains(line, "[BATCH]") {
		return HighlightStyle.Render(line)
	}
	if strings.Contains(line, "[FAILED]") || strings.Contains(line, "[ERROR]") {
		return DangerStyle.Render(line)
	}
	if strings.Contains(line, "[VERIFY]") || strings.Contains(line, "[UPLOADED]") {
		return WarningStyle.Render(line)
	}
	if strings.Contains(line, "[rclone]") {
		return MutedStyle.Render(line)
	}
	return NormalRowStyle.Render(line)
}

func (m *Model) viewTransferring() string {
	totalWidth := m.width
	if totalWidth <= 0 {
		totalWidth = 100
	}
	totalHeight := m.height
	if totalHeight <= 0 {
		totalHeight = 26
	}

	gutter := 2
	isSideBySide := totalWidth >= 80

	var paneWidth int
	if isSideBySide {
		paneWidth = (totalWidth - gutter) / 2
	} else {
		paneWidth = totalWidth - 2
	}
	if paneWidth < 38 {
		paneWidth = 38
	}

	// Calculate inner content width:
	// Styles have rounded border (1 left + 1 right = 2) and padding (2 left + 2 right = 4) => 6
	innerW := paneWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	// Calculate target height for both panels so their borders and bottom align perfectly!
	targetHeight := totalHeight - 2
	if targetHeight < 20 {
		targetHeight = 20
	}
	// Inner height for content: border (1 top + 1 bottom = 2) and padding (1 top + 1 bottom = 2) => 4
	innerH := targetHeight - 4
	if innerH < 14 {
		innerH = 14
	}

	// Dynamically scale the progress bars to fill the left panel
	progWidth := innerW - 4
	if progWidth > 15 {
		m.fileProgress.Width = progWidth
		m.overallProgress.Width = progWidth
	}

	// 1. Left Box: Progress & Status
	var left strings.Builder
	left.WriteString(TitleStyle.Render("GMOVE — TRANSFERRING") + "\n")
	left.WriteString(strings.Repeat("─", innerW) + "\n\n")

	curName := m.currentFile
	if curName == "" {
		curName = "Preparing operation in database..."
	}
	left.WriteString(NormalRowStyle.Render("Current file:") + "\n")
	left.WriteString(HighlightStyle.Render("  "+truncate(curName, innerW-4)) + "\n\n")

	var filePercent float64
	if m.currentTotal > 0 {
		filePercent = float64(m.currentBytes) / float64(m.currentTotal)
	}
	left.WriteString(m.fileProgress.ViewAs(filePercent) + "\n")
	left.WriteString(fmt.Sprintf("  %s / %s   Speed: %s   ETA: %s\n\n",
		utils.FormatBytes(m.currentBytes),
		utils.FormatBytes(m.currentTotal),
		utils.FormatSpeed(m.currentSpeed),
		utils.FormatETA(m.currentETA),
	))

	left.WriteString(NormalRowStyle.Render("Overall batch:") + "\n")
	var overallPercent float64
	if m.overallTotal > 0 {
		overallPercent = float64(m.overallBytes) / float64(m.overallTotal)
	}
	left.WriteString(m.overallProgress.ViewAs(overallPercent) + "\n")
	left.WriteString(fmt.Sprintf("  %s / %s\n\n",
		utils.FormatBytes(m.overallBytes),
		utils.FormatBytes(m.overallTotal),
	))

	left.WriteString(NormalRowStyle.Render("Status:") + "\n")
	left.WriteString(SecondaryStyle.Render(fmt.Sprintf("  ✓ %d completed\n", m.completedCount)))
	left.WriteString(HighlightStyle.Render(fmt.Sprintf("  ↑ %d transferring\n", m.transferringCount)))
	left.WriteString(MutedStyle.Render(fmt.Sprintf("  ⟳ %d pending\n", m.pendingCount)))
	if m.failedCount > 0 {
		left.WriteString(BadgeDanger.Render(fmt.Sprintf("  ✗ %d failed\n", m.failedCount)))
	}

	left.WriteString("\n" + HelpStyle.Render("Press q to cancel. No local files deleted."))
	leftPanel := TransferLeftBoxStyle.Width(innerW).Height(innerH).Render(left.String())

	// 2. Right Box: Live Activity & Rclone Log
	var right strings.Builder
	right.WriteString(HighlightStyle.Bold(true).Render("LIVE ACTIVITY & RCLONE LOG") + "\n")
	right.WriteString(strings.Repeat("─", innerW) + "\n\n")

	// Calculate maximum log lines based on available inner height
	// Title (1) + Hr (1) + blank (1) = 3 lines at top
	maxLines := innerH - 4
	if maxLines < 5 {
		maxLines = 5
	}

	if len(m.logLines) == 0 {
		right.WriteString(MutedStyle.Render("  Connecting to rclone remote...\n  Waiting for operation events...\n"))
	} else {
		start := 0
		if len(m.logLines) > maxLines {
			start = len(m.logLines) - maxLines
		}
		for i := start; i < len(m.logLines); i++ {
			right.WriteString(formatLogLine(m.logLines[i], innerW-2) + "\n")
		}
	}

	rightPanel := LogBoxStyle.Width(innerW).Height(innerH).Render(right.String())

	if isSideBySide {
		return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, strings.Repeat(" ", gutter), rightPanel)
	}
	return lipgloss.JoinVertical(lipgloss.Left, leftPanel, rightPanel)
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
