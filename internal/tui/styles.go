package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Vibrant theme — colorful, bold, personality.

var (
	// Brand.
	FlintPurple = lipgloss.Color("#818cf8")
	FlintIndigo = lipgloss.Color("#6366f1")

	// Status colors.
	ColorPassed  = lipgloss.Color("#4ade80") // green
	ColorFailed  = lipgloss.Color("#f87171") // red
	ColorRunning = lipgloss.Color("#facc15") // yellow
	ColorWaiting = lipgloss.Color("#6b7280") // gray
	ColorSkipped = lipgloss.Color("#9ca3af") // light gray

	// Structural.
	ColorBorder = lipgloss.Color("#374151")
	ColorHeader = lipgloss.Color("#e5e7eb")
	ColorDim    = lipgloss.Color("#6b7280")
	ColorAccent = FlintPurple

	// Styles.
	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder)

	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorHeader)

	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(FlintPurple)

	DimStyle = lipgloss.NewStyle().
			Foreground(ColorDim)

	SelectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e1b4b")).
			Foreground(lipgloss.Color("#e0e7ff"))

	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorDim)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#fca5a5")).
			Background(lipgloss.Color("#7f1d1d")).
			Padding(0, 1)
)

// StatusSymbol returns a vibrant status symbol with color.
func StatusSymbol(status string) string {
	switch status {
	case "succeeded":
		return lipgloss.NewStyle().Foreground(ColorPassed).Render("✅")
	case "failed":
		return lipgloss.NewStyle().Foreground(ColorFailed).Render("❌")
	case "running":
		return lipgloss.NewStyle().Foreground(ColorRunning).Render("🔄")
	case "pending", "queued":
		return lipgloss.NewStyle().Foreground(ColorWaiting).Render("◌")
	case "skipped":
		return lipgloss.NewStyle().Foreground(ColorSkipped).Render("⊘")
	case "waiting":
		return lipgloss.NewStyle().Foreground(ColorRunning).Render("⏸")
	case "cancelled":
		return lipgloss.NewStyle().Foreground(ColorDim).Render("⊘")
	default:
		return "?"
	}
}

// StatusText returns a styled status text.
func StatusText(status string) string {
	switch status {
	case "succeeded":
		return lipgloss.NewStyle().Foreground(ColorPassed).Render("passed")
	case "failed":
		return lipgloss.NewStyle().Foreground(ColorFailed).Render("failed")
	case "running":
		return lipgloss.NewStyle().Foreground(ColorRunning).Render("running")
	case "pending", "queued":
		return lipgloss.NewStyle().Foreground(ColorWaiting).Render("pending")
	case "waiting":
		return lipgloss.NewStyle().Foreground(ColorRunning).Render("waiting")
	case "cancelled":
		return lipgloss.NewStyle().Foreground(ColorDim).Render("cancelled")
	default:
		return status
	}
}

// ProjectDot returns a colored dot for a project.
func ProjectDot(colour string) string {
	if colour == "" {
		colour = "#6366f1"
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(colour)).Render("●")
}

// FormatDuration formats milliseconds to a human-readable string.
func FormatDuration(ms *int) string {
	if ms == nil {
		return ""
	}
	secs := *ms / 1000
	if secs < 60 {
		return lipgloss.NewStyle().Foreground(ColorDim).Render(
			fmt.Sprintf("%ds", secs))
	}
	mins := secs / 60
	if mins < 60 {
		return lipgloss.NewStyle().Foreground(ColorDim).Render(
			fmt.Sprintf("%dm", mins))
	}
	hours := mins / 60
	return lipgloss.NewStyle().Foreground(ColorDim).Render(
		fmt.Sprintf("%dh", hours))
}

// Truncate truncates a string to max length with ellipsis.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
