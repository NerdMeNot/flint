package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// View is the interface for each TUI screen.
type View interface {
	Init() tea.Cmd
	Update(msg tea.Msg) (View, tea.Cmd)
	View() string
	SetSize(w, h int)
	ShortHelp() string
}

// StartOption configures how the TUI starts.
type StartOption struct {
	RunID   string // --run flag
	Project string // --project flag
	Gates   bool   // --gates flag
}

// App is the root Bubble Tea model.
type App struct {
	client *Client
	org    *Org
	width  int
	height int
	stack  []View
	err    error
	errAt  time.Time
}

// NewApp creates a new TUI application.
func NewApp(client *Client, opts StartOption) *App {
	app := &App{client: client}

	// Determine initial view.
	if opts.Gates {
		app.stack = []View{NewGatesView(client)}
	} else if opts.RunID != "" {
		app.stack = []View{NewRunDetailView(client, opts.RunID)}
	} else {
		app.stack = []View{NewDashboardView(client)}
	}

	return app
}

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{
		a.fetchOrg(),
		a.currentView().Init(),
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		contentH := a.height - 4 // header + footer
		for _, v := range a.stack {
			v.SetSize(a.width-2, contentH)
		}
		return a, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return a, tea.Quit
		case "?":
			if _, ok := a.currentView().(*HelpView); !ok {
				return a, a.push(NewHelpView())
			}
		case "q", "esc":
			if len(a.stack) <= 1 {
				return a, tea.Quit
			}
			return a, a.pop()
		}

	case pushViewMsg:
		return a, a.push(msg.view)
	case popViewMsg:
		if len(a.stack) <= 1 {
			return a, tea.Quit
		}
		return a, a.pop()

	case orgLoadedMsg:
		if msg.err == nil {
			a.org = msg.org
		}
		return a, nil

	case errMsg:
		a.err = msg.err
		a.errAt = time.Now()
		return a, nil
	}

	// Delegate to current view.
	if len(a.stack) > 0 {
		v, cmd := a.currentView().Update(msg)
		a.stack[len(a.stack)-1] = v
		return a, cmd
	}

	return a, nil
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	// Header.
	orgName := "flint"
	if a.org != nil {
		orgName = a.org.Name
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		TitleStyle.Render("⚡ "+orgName),
		lipgloss.NewStyle().Width(a.width-lipgloss.Width(orgName)-20).Render(""),
		DimStyle.Render("? help  q quit"),
	)

	// Content.
	content := ""
	if len(a.stack) > 0 {
		content = a.currentView().View()
	}

	// Footer.
	footer := ""
	if len(a.stack) > 0 {
		footer = FooterStyle.Render(a.currentView().ShortHelp())
	}

	// Error bar.
	if a.err != nil && time.Since(a.errAt) < 5*time.Second {
		footer = ErrorStyle.Render("Error: " + a.err.Error())
	}

	// Compose.
	box := BorderStyle.Width(a.width - 2).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			header,
			lipgloss.NewStyle().Height(a.height-6).Render(content),
			footer,
		),
	)

	return box
}

func (a *App) currentView() View {
	if len(a.stack) == 0 {
		return nil
	}
	return a.stack[len(a.stack)-1]
}

func (a *App) push(v View) tea.Cmd {
	a.stack = append(a.stack, v)
	v.SetSize(a.width-2, a.height-4)
	return v.Init()
}

func (a *App) pop() tea.Cmd {
	if len(a.stack) > 1 {
		a.stack = a.stack[:len(a.stack)-1]
	}
	return nil
}

func (a *App) fetchOrg() tea.Cmd {
	return func() tea.Msg {
		org, err := a.client.GetOrg(context.Background())
		return orgLoadedMsg{org: org, err: err}
	}
}

type errMsg struct{ err error }
