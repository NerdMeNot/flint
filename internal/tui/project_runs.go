package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProjectRunsView shows the run history for a single project.
type ProjectRunsView struct {
	client  *Client
	project Project
	runs    []Run
	cursor  int
	loading bool
	width   int
	height  int
}

func NewProjectRunsView(client *Client, project Project) *ProjectRunsView {
	return &ProjectRunsView{client: client, project: project, loading: true}
}

func (v *ProjectRunsView) Init() tea.Cmd {
	return v.fetchRuns()
}

func (v *ProjectRunsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.runs)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "enter":
			if len(v.runs) > 0 {
				r := v.runs[v.cursor]
				return v, func() tea.Msg {
					return pushViewMsg{view: NewRunDetailView(v.client, r.ID)}
				}
			}
		case "r":
			return v, func() tea.Msg {
				return pushViewMsg{view: NewTriggerView(v.client, v.project)}
			}
		}

	case runsLoadedMsg:
		v.loading = false
		if msg.err == nil {
			v.runs = msg.runs
		}
		return v, v.scheduleTick()

	case tickMsg:
		return v, v.fetchRuns()
	}

	return v, nil
}

func (v *ProjectRunsView) View() string {
	header := TitleStyle.Render(fmt.Sprintf("  %s %s", ProjectDot(v.project.Colour), v.project.DisplayName))

	if v.loading && len(v.runs) == 0 {
		return header + "\n\n" + DimStyle.Render("  Loading runs...")
	}

	if len(v.runs) == 0 {
		return header + "\n\n" + DimStyle.Render("  No runs yet.")
	}

	// Column header.
	colHeader := DimStyle.Render(fmt.Sprintf("  %-4s %-12s %-8s %-15s %-10s %s",
		"", "WORKFLOW", "TRIGGER", "BRANCH", "SHA", "DURATION"))

	var rows []string
	rows = append(rows, header, "", colHeader)

	for i, r := range v.runs {
		sha := ""
		if r.CommitSHA != nil && len(*r.CommitSHA) >= 7 {
			sha = (*r.CommitSHA)[:7]
		}
		branch := ""
		if r.TriggerRef != nil {
			branch = Truncate(*r.TriggerRef, 15)
		}

		row := fmt.Sprintf("  %s %-12s %-8s %-15s %-10s %s",
			StatusSymbol(r.Status),
			Truncate(r.WorkflowFile, 12),
			Truncate(r.TriggerType, 8),
			branch, sha,
			FormatDuration(r.DurationMs),
		)

		if i == v.cursor {
			row = SelectedStyle.Width(v.width).Render(row)
		}
		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (v *ProjectRunsView) SetSize(w, h int) { v.width = w; v.height = h }

func (v *ProjectRunsView) ShortHelp() string {
	return "  ↑↓ navigate  enter view run  r trigger  esc back"
}

func (v *ProjectRunsView) fetchRuns() tea.Cmd {
	return func() tea.Msg {
		runs, err := v.client.ListRuns(context.Background(), v.project.ID)
		return runsLoadedMsg{projectID: v.project.ID, runs: runs, err: err}
	}
}

func (v *ProjectRunsView) scheduleTick() tea.Cmd {
	for _, r := range v.runs {
		if r.Status == "running" || r.Status == "pending" {
			return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return tickMsg(t)
			})
		}
	}
	return nil
}
