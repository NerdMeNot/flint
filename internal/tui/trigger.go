package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// TriggerView provides a form to manually trigger a pipeline run: branch,
// workflow file, and environment are editable (tab cycles fields).
type TriggerView struct {
	client  *Client
	project Project
	fields  [3]string // branch, workflow, environment
	labels  [3]string
	focus   int
	width   int
	height  int
	errMsg  string
}

func NewTriggerView(client *Client, project Project) *TriggerView {
	v := &TriggerView{
		client:  client,
		project: project,
		labels:  [3]string{"Branch", "Workflow", "Environment"},
	}
	v.fields[0] = project.DefaultBranch
	v.fields[1] = "ci.yaml"
	return v
}

func (v *TriggerView) Init() tea.Cmd { return nil }

func (v *TriggerView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return v, v.triggerRun()
		case "tab", "down":
			v.focus = (v.focus + 1) % len(v.fields)
		case "shift+tab", "up":
			v.focus = (v.focus + len(v.fields) - 1) % len(v.fields)
		case "backspace":
			if f := v.fields[v.focus]; f != "" {
				v.fields[v.focus] = f[:len(f)-1]
			}
		default:
			if len(msg.Runes) > 0 && msg.Type == tea.KeyRunes {
				v.fields[v.focus] += string(msg.Runes)
			}
		}

	case runTriggeredMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			return v, nil
		}
		if msg.run != nil {
			return v, func() tea.Msg {
				return popViewMsg{}
			}
		}
	}
	return v, nil
}

func (v *TriggerView) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("  🚀 Trigger Run") + "\n\n")
	b.WriteString("  Project:     " + v.project.DisplayName + "\n")
	for i, label := range v.labels {
		cursor := " "
		if i == v.focus {
			cursor = "▎"
		}
		b.WriteString("  " + cursor + padRight(label+":", 12) + v.fields[i] + "\n")
	}
	if v.errMsg != "" {
		b.WriteString("\n" + ErrorStyle.Render("  "+v.errMsg) + "\n")
	}
	b.WriteString("\n" + DimStyle.Render("  tab next field · enter trigger · esc cancel"))
	return b.String()
}

func padRight(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func (v *TriggerView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *TriggerView) ShortHelp() string { return "  tab field  enter trigger  esc cancel" }

func (v *TriggerView) triggerRun() tea.Cmd {
	branch, workflow, env := v.fields[0], v.fields[1], v.fields[2]
	return func() tea.Msg {
		run, err := v.client.TriggerRun(context.Background(), v.project.ID, branch, workflow, env)
		return runTriggeredMsg{run: run, err: err}
	}
}
