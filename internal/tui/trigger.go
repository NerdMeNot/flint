package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
)

// TriggerView provides a form to manually trigger a pipeline run.
type TriggerView struct {
	client   *Client
	project  Project
	branch   string
	workflow string
	width    int
	height   int
}

func NewTriggerView(client *Client, project Project) *TriggerView {
	return &TriggerView{
		client:   client,
		project:  project,
		branch:   project.DefaultBranch,
		workflow: "ci.yaml",
	}
}

func (v *TriggerView) Init() tea.Cmd { return nil }

func (v *TriggerView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			return v, v.triggerRun()
		}

	case runTriggeredMsg:
		if msg.err == nil && msg.run != nil {
			return v, func() tea.Msg {
				return popViewMsg{}
			}
		}
	}
	return v, nil
}

func (v *TriggerView) View() string {
	return TitleStyle.Render("  🚀 Trigger Run") + "\n\n" +
		"  Project:  " + v.project.DisplayName + "\n" +
		"  Branch:   " + v.branch + "\n" +
		"  Workflow: " + v.workflow + "\n\n" +
		DimStyle.Render("  Press enter to trigger, esc to cancel")
}

func (v *TriggerView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *TriggerView) ShortHelp() string { return "  enter trigger  esc cancel" }

func (v *TriggerView) triggerRun() tea.Cmd {
	return func() tea.Msg {
		run, err := v.client.TriggerRun(context.Background(), v.project.ID, v.branch, v.workflow)
		return runTriggeredMsg{run: run, err: err}
	}
}
