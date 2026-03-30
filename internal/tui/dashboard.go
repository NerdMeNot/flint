package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashboardView shows the project list with latest run status.
type DashboardView struct {
	client     *Client
	projects   []Project
	latestRuns map[string]*Run // projectID → latest run
	cursor     int
	loading    bool
	width      int
	height     int
}

// NewDashboardView creates the dashboard.
func NewDashboardView(client *Client) *DashboardView {
	return &DashboardView{
		client:     client,
		latestRuns: make(map[string]*Run),
		loading:    true,
	}
}

func (d *DashboardView) Init() tea.Cmd {
	return d.fetchProjects()
}

func (d *DashboardView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if d.cursor < len(d.projects)-1 {
				d.cursor++
			}
		case "k", "up":
			if d.cursor > 0 {
				d.cursor--
			}
		case "enter":
			if len(d.projects) > 0 {
				p := d.projects[d.cursor]
				return d, func() tea.Msg {
					return pushViewMsg{view: NewProjectRunsView(d.client, p)}
				}
			}
		case "r":
			// Trigger run for selected project.
			if len(d.projects) > 0 {
				return d, func() tea.Msg {
					return pushViewMsg{view: NewTriggerView(d.client, d.projects[d.cursor])}
				}
			}
		case "a":
			return d, func() tea.Msg {
				return pushViewMsg{view: NewGatesView(d.client)}
			}
		}

	case projectsLoadedMsg:
		d.loading = false
		if msg.err == nil {
			d.projects = msg.projects
			d.latestRuns = msg.runs
		}
		return d, d.scheduleTick()

	case tickMsg:
		return d, d.fetchProjects()
	}

	return d, nil
}

func (d *DashboardView) View() string {
	if d.loading && len(d.projects) == 0 {
		return DimStyle.Render("  Loading projects...")
	}

	if len(d.projects) == 0 {
		return DimStyle.Render("  No projects found. Create one via Pipeline CRD or UI.")
	}

	var rows []string
	for i, p := range d.projects {
		run := d.latestRuns[p.ID]

		// Project dot + name.
		dot := ProjectDot(p.Colour)
		name := Truncate(p.DisplayName, 20)

		// Latest run info.
		status := "  "
		branch := ""
		duration := ""
		if run != nil {
			status = StatusSymbol(run.Status)
			if run.TriggerRef != nil {
				branch = Truncate(*run.TriggerRef, 15)
			}
			duration = FormatDuration(run.DurationMs)
		}

		// Build row.
		row := fmt.Sprintf("  %s %-20s  %s %-8s  %-15s  %s",
			dot, name, status, StatusText(runStatus(run)), branch, duration)

		// Highlight selected.
		if i == d.cursor {
			row = SelectedStyle.Width(d.width).Render(row)
		}

		rows = append(rows, row)
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func (d *DashboardView) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *DashboardView) ShortHelp() string {
	return "  ↑↓ navigate  enter select  r run  a gates  / search  ? help"
}

func (d *DashboardView) fetchProjects() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		projects, err := d.client.ListProjects(ctx)
		if err != nil {
			return projectsLoadedMsg{err: err}
		}

		runs := make(map[string]*Run, len(projects))
		for _, p := range projects {
			runList, err := d.client.ListRuns(ctx, p.ID)
			if err == nil && len(runList) > 0 {
				r := runList[0]
				runs[p.ID] = &r
			}
		}

		return projectsLoadedMsg{projects: projects, runs: runs}
	}
}

func (d *DashboardView) scheduleTick() tea.Cmd {
	// Only tick if there are running projects.
	hasRunning := false
	for _, r := range d.latestRuns {
		if r != nil && (r.Status == "running" || r.Status == "pending") {
			hasRunning = true
			break
		}
	}
	if !hasRunning {
		return nil
	}
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func runStatus(r *Run) string {
	if r == nil {
		return ""
	}
	return r.Status
}
