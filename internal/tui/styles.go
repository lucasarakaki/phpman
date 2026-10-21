package tui

import "github.com/charmbracelet/lipgloss"

// Adaptive colors keep contrast in both light and dark terminals.
var (
	colorAccent = lipgloss.AdaptiveColor{Light: "#6f42c1", Dark: "#c792ea"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#6a737d", Dark: "#8b949e"}
	colorText   = lipgloss.AdaptiveColor{Light: "#24292f", Dark: "#e6edf3"}
	colorOK     = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#7ee787"}
	colorWarn   = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#e3b341"}
	colorErr    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#ff7b72"}
	colorSelect = lipgloss.AdaptiveColor{Light: "#efe7fd", Dark: "#2b2238"}
	colorBorder = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#3d444d"}
)

var (
	logoStyle    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorText)
	mutedStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	textStyle    = lipgloss.NewStyle().Foreground(colorText)
	okStyle      = lipgloss.NewStyle().Foreground(colorOK)
	warnStyle    = lipgloss.NewStyle().Foreground(colorWarn)
	errStyle     = lipgloss.NewStyle().Foreground(colorErr)
	accentStyle  = lipgloss.NewStyle().Foreground(colorAccent)
	labelStyle   = lipgloss.NewStyle().Foreground(colorMuted)
	activeTab    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(colorAccent).Padding(0, 1)
	inactiveTab  = lipgloss.NewStyle().Foreground(colorMuted).Padding(0, 1)
	cardStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorBorder).Padding(0, 1)
	selectedItem = lipgloss.NewStyle().Background(colorSelect).Foreground(colorText)
	dividerStyle = lipgloss.NewStyle().Foreground(colorBorder)
)
