package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// StepLogsView shows logs for a single step.
type StepLogsView struct {
	stepName string
	runID    string
	content  string
	width    int
	height   int
}

func NewStepLogsView(stepName, runID string) *StepLogsView {
	return &StepLogsView{stepName: stepName, runID: runID}
}

func (v *StepLogsView) Init() tea.Cmd { return nil }

func (v *StepLogsView) Update(msg tea.Msg) (View, tea.Cmd) {
	return v, nil
}

func (v *StepLogsView) View() string {
	return fmt.Sprintf(
		"  %s\n\n  %s\n  %s",
		TitleStyle.Render("📋 Logs: "+v.stepName),
		DimStyle.Render("Live log streaming coming soon."),
		DimStyle.Render("Run ID: "+v.runID),
	)
}

func (v *StepLogsView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *StepLogsView) ShortHelp() string { return "  esc back" }
