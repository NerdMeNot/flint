package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpView shows all key bindings.
type HelpView struct {
	width  int
	height int
}

func NewHelpView() *HelpView {
	return &HelpView{}
}

func (v *HelpView) Init() tea.Cmd { return nil }

func (v *HelpView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		// Any key dismisses help.
		return v, func() tea.Msg { return popViewMsg{} }
	}
	return v, nil
}

func (v *HelpView) View() string {
	title := TitleStyle.Render("  ⌨ Key Bindings")

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(FlintPurple).Width(12)
	descStyle := lipgloss.NewStyle().Foreground(ColorHeader)

	bindings := []struct{ key, desc string }{
		{"↑/↓ j/k", "Navigate"},
		{"enter", "Select / drill down"},
		{"esc / q", "Go back / quit"},
		{"r", "Trigger a run"},
		{"l", "View step logs"},
		{"a", "Approve gate / view gates"},
		{"?", "Toggle this help"},
		{"ctrl+c", "Force quit"},
	}

	rows := []string{title, ""}
	for _, b := range bindings {
		rows = append(rows, "  "+keyStyle.Render(b.key)+descStyle.Render(b.desc))
	}
	rows = append(rows, "", DimStyle.Render("  Press any key to dismiss"))

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (v *HelpView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *HelpView) ShortHelp() string { return "  any key to dismiss" }
