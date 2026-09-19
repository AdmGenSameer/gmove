package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Palette
	ColorPrimary   = lipgloss.Color("#7D56F4") // Vibrant Purple
	ColorSecondary = lipgloss.Color("#04B575") // Vibrant Emerald Green
	ColorWarning   = lipgloss.Color("#FFB86C") // Amber / Orange
	ColorDanger    = lipgloss.Color("#FF5555") // Red
	ColorMuted     = lipgloss.Color("#6272A4") // Muted Slate
	ColorSubtle    = lipgloss.Color("#44475A") // Dark Grey
	ColorText      = lipgloss.Color("#F8F8F2") // Off-white
	ColorHighlight = lipgloss.Color("#8BE9FD") // Cyan

	// Header Styles
	HeaderBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(0, 1).
			Foreground(ColorText).
			Bold(true)

	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorPrimary).
			Bold(true)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Italic(true)

	// Panel Styles
	PanelStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorSubtle).
			Padding(1, 2)

	WarningBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorDanger).
			Padding(1, 2)

	// Table & List Styles
	SelectedRowStyle = lipgloss.NewStyle().
				Foreground(ColorHighlight).
				Bold(true)

	NormalRowStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	RadioActiveStyle = lipgloss.NewStyle().
				Foreground(ColorSecondary).
				Bold(true)

	RadioInactiveStyle = lipgloss.NewStyle().
				Foreground(ColorMuted)

	// Status Badges
	BadgeSuccess = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true)

	BadgeWarning = lipgloss.NewStyle().
			Foreground(ColorWarning).
			Bold(true)

	BadgeDanger = lipgloss.NewStyle().
			Foreground(ColorDanger).
			Bold(true)

	// Direct text styling
	MutedStyle     = lipgloss.NewStyle().Foreground(ColorMuted)
	WarningStyle   = lipgloss.NewStyle().Foreground(ColorWarning)
	SecondaryStyle = lipgloss.NewStyle().Foreground(ColorSecondary)
	HighlightStyle = lipgloss.NewStyle().Foreground(ColorHighlight)
	DangerStyle    = lipgloss.NewStyle().Foreground(ColorDanger)

	// Keymap Helper Style
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// Activity Log Box Style
	LogBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(ColorHighlight).
			Padding(1, 2)
)
