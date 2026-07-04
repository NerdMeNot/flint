package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// GatesView shows pending gate approvals across all projects and lets the
// operator approve (a) or reject (r) the selected gate.
type GatesView struct {
	client  *Client
	gates   []PendingGate
	cursor  int
	loading bool
	errMsg  string
	notice  string
	width   int
	height  int
}

func NewGatesView(client *Client) *GatesView {
	return &GatesView{client: client, loading: true}
}

func (v *GatesView) Init() tea.Cmd { return v.load() }

func (v *GatesView) load() tea.Cmd {
	return func() tea.Msg {
		gates, err := v.client.ListPendingGates(context.Background())
		return gatesLoadedMsg{gates: gates, err: err}
	}
}

func (v *GatesView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case gatesLoadedMsg:
		v.loading = false
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			return v, nil
		}
		v.errMsg = ""
		v.gates = msg.gates
		if v.cursor >= len(v.gates) {
			v.cursor = max(0, len(v.gates)-1)
		}
		return v, nil

	case gateApprovedMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			return v, nil
		}
		v.notice = "✓ done"
		return v, v.load()

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.gates)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "a", "enter":
			if g := v.selected(); g != nil {
				return v, v.approve(*g)
			}
		case "r":
			if g := v.selected(); g != nil {
				return v, v.reject(*g)
			}
		case "R":
			v.loading = true
			return v, v.load()
		}
	}
	return v, nil
}

func (v *GatesView) selected() *PendingGate {
	if v.cursor < len(v.gates) {
		return &v.gates[v.cursor]
	}
	return nil
}

func (v *GatesView) approve(g PendingGate) tea.Cmd {
	return func() tea.Msg {
		return gateApprovedMsg{err: v.client.ApproveGate(context.Background(), g.RunID, g.StepName)}
	}
}

func (v *GatesView) reject(g PendingGate) tea.Cmd {
	return func() tea.Msg {
		return gateApprovedMsg{err: v.client.RejectGate(context.Background(), g.RunID, g.StepName, "rejected from TUI")}
	}
}

func (v *GatesView) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("  ⏸ Pending Approvals") + "\n\n")

	switch {
	case v.loading:
		b.WriteString(DimStyle.Render("  loading…"))
	case v.errMsg != "":
		b.WriteString(ErrorStyle.Render("  " + v.errMsg))
	case len(v.gates) == 0:
		b.WriteString(DimStyle.Render("  No pending gates."))
	default:
		for i, g := range v.gates {
			cursor := "  "
			if i == v.cursor {
				cursor = "▎ "
			}
			fmt.Fprintf(&b, "%s%s  %s  %s\n", cursor, g.StepName, g.Project, DimStyle.Render(g.Branch))
			if i == v.cursor && g.Message != "" {
				fmt.Fprintf(&b, "     %s\n", DimStyle.Render(g.Message))
			}
		}
	}
	if v.notice != "" {
		b.WriteString("\n" + DimStyle.Render("  "+v.notice))
	}
	return b.String()
}

func (v *GatesView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *GatesView) ShortHelp() string { return "  a approve  r reject  R refresh  esc back" }
