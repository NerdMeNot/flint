package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// GatesView shows pending gate approvals across all projects.
type GatesView struct {
	client  *Client
	gates   []PendingGate
	cursor  int
	loading bool
	width   int
	height  int
}

func NewGatesView(client *Client) *GatesView {
	return &GatesView{client: client, loading: true}
}

func (v *GatesView) Init() tea.Cmd { return nil }

func (v *GatesView) Update(msg tea.Msg) (View, tea.Cmd) {
	return v, nil
}

func (v *GatesView) View() string {
	return TitleStyle.Render("  ⏸ Pending Approvals") + "\n\n" +
		DimStyle.Render("  No pending gates. (Gate approval endpoint coming soon.)")
}

func (v *GatesView) SetSize(w, h int) { v.width = w; v.height = h }
func (v *GatesView) ShortHelp() string { return "  enter approve  esc back" }
