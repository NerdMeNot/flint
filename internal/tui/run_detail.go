package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RunDetailView shows a single run with DAG visualization.
type RunDetailView struct {
	client       *Client
	runID        string
	run          *Run
	state        *WorkflowState
	selectedStep int
	loading      bool
	width, height int
}

func NewRunDetailView(client *Client, runID string) *RunDetailView {
	return &RunDetailView{client: client, runID: runID, loading: true}
}

func (v *RunDetailView) Init() tea.Cmd {
	return v.fetchDetail()
}

func (v *RunDetailView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.state != nil && v.selectedStep < len(v.state.Steps)-1 {
				v.selectedStep++
			}
		case "k", "up":
			if v.selectedStep > 0 {
				v.selectedStep--
			}
		case "l", "enter":
			if v.state != nil && len(v.state.Steps) > 0 {
				step := v.state.Steps[v.selectedStep]
				return v, func() tea.Msg {
					return pushViewMsg{view: NewStepLogsView(step.Name, v.runID)}
				}
			}
		case "a":
			// Approve gate if selected step is waiting.
			if v.state != nil && len(v.state.Steps) > 0 {
				step := v.state.Steps[v.selectedStep]
				if step.Status == "waiting" {
					return v, v.approveGate(step.Name)
				}
			}
		}

	case runDetailLoadedMsg:
		v.loading = false
		if msg.err == nil {
			v.run = msg.run
			v.state = msg.state
		}
		return v, v.scheduleTick()

	case tickMsg:
		return v, v.fetchDetail()

	case gateApprovedMsg:
		return v, v.fetchDetail()
	}

	return v, nil
}

func (v *RunDetailView) View() string {
	if v.loading && v.run == nil {
		return DimStyle.Render("  Loading run detail...")
	}

	var lines []string

	// Header.
	status := ""
	if v.run != nil {
		status = StatusSymbol(v.run.Status) + " " + StatusText(v.run.Status)
	}
	sha := ""
	if v.run != nil && v.run.CommitSHA != nil && len(*v.run.CommitSHA) >= 7 {
		sha = (*v.run.CommitSHA)[:7]
	}
	branch := ""
	if v.run != nil && v.run.TriggerRef != nil {
		branch = *v.run.TriggerRef
	}

	header := fmt.Sprintf("  %s  %s · %s · %s  %s",
		TitleStyle.Render(v.runID[:8]),
		DimStyle.Render(branch),
		DimStyle.Render(sha),
		status,
		FormatDuration(v.run.DurationMs),
	)
	lines = append(lines, header, "")

	// DAG / step list.
	if v.state != nil && len(v.state.Steps) > 0 {
		lines = append(lines, renderStepList(v.state.Steps, v.selectedStep, v.width))
	} else {
		lines = append(lines, DimStyle.Render("  No steps"))
	}

	// Selected step detail.
	if v.state != nil && len(v.state.Steps) > 0 && v.selectedStep < len(v.state.Steps) {
		step := v.state.Steps[v.selectedStep]
		lines = append(lines, "")
		detail := fmt.Sprintf("  Selected: %s  │  wave %d  │  attempt %d",
			lipgloss.NewStyle().Bold(true).Render(step.Name),
			step.Wave, step.Attempt)
		if step.Error != "" {
			detail += "  │  " + lipgloss.NewStyle().Foreground(ColorFailed).Render(Truncate(step.Error, 40))
		}
		lines = append(lines, DimStyle.Render(detail))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (v *RunDetailView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *RunDetailView) ShortHelp() string {
	return "  ↑↓ select step  l/enter logs  a approve  esc back"
}

func (v *RunDetailView) fetchDetail() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		run, err := v.client.GetRun(ctx, v.runID)
		if err != nil {
			return runDetailLoadedMsg{err: err}
		}
		state, _ := v.client.GetRunSteps(ctx, v.runID)
		return runDetailLoadedMsg{run: run, state: state}
	}
}

func (v *RunDetailView) scheduleTick() tea.Cmd {
	if v.run != nil && (v.run.Status == "running" || v.run.Status == "pending") {
		return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
			return tickMsg(t)
		})
	}
	return nil
}

func (v *RunDetailView) approveGate(stepName string) tea.Cmd {
	return func() tea.Msg {
		err := v.client.ApproveGate(context.Background(), v.runID, stepName)
		return gateApprovedMsg{err: err}
	}
}

// renderStepList renders steps grouped by wave.
func renderStepList(steps []StepState, selected, width int) string {
	var rows []string

	// Group steps by wave.
	waves := make(map[int][]int) // wave → step indices
	for i, s := range steps {
		waves[s.Wave] = append(waves[s.Wave], i)
	}

	// Find max wave.
	maxWave := 0
	for w := range waves {
		if w > maxWave {
			maxWave = w
		}
	}

	for w := 0; w <= maxWave; w++ {
		indices, ok := waves[w]
		if !ok {
			continue
		}
		if w > 0 {
			rows = append(rows, DimStyle.Render("    │"))
		}

		for _, idx := range indices {
			s := steps[idx]
			connector := "├─"
			if idx == indices[len(indices)-1] {
				connector = "└─"
			}
			if len(indices) == 1 {
				connector = "──"
			}

			row := fmt.Sprintf("  %s %s %-20s  %s",
				DimStyle.Render(connector),
				StatusSymbol(s.Status),
				s.Name,
				StatusText(s.Status),
			)

			if idx == selected {
				row = SelectedStyle.Width(width).Render(row)
			}

			rows = append(rows, row)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
