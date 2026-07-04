package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// stepLogsLoadedMsg carries a poll's worth of log lines.
type stepLogsLoadedMsg struct {
	lines    []LogLine
	complete bool
	err      error
}

// StepLogsView shows a step's logs, polling while the step is running.
// j/k or arrows scroll; G jumps to the tail (and re-enables follow).
type StepLogsView struct {
	client   *Client
	stepName string
	runID    string
	lines    []string
	complete bool
	errMsg   string
	offset   int
	follow   bool
	width    int
	height   int
}

func NewStepLogsView(client *Client, stepName, runID string) *StepLogsView {
	return &StepLogsView{client: client, stepName: stepName, runID: runID, follow: true}
}

func (v *StepLogsView) Init() tea.Cmd { return v.fetch() }

func (v *StepLogsView) fetch() tea.Cmd {
	return func() tea.Msg {
		lines, complete, err := v.client.GetStepLogs(context.Background(), v.runID, v.stepName)
		return stepLogsLoadedMsg{lines: lines, complete: complete, err: err}
	}
}

func (v *StepLogsView) pollLater() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg {
		return tickMsg(time.Now())
	})
}

func (v *StepLogsView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case stepLogsLoadedMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			if !v.complete {
				return v, v.pollLater()
			}
			return v, nil
		}
		v.errMsg = ""
		v.lines = v.lines[:0]
		for _, l := range msg.lines {
			v.lines = append(v.lines, l.Content)
		}
		v.complete = msg.complete
		if v.follow {
			v.scrollToEnd()
		}
		if !v.complete {
			return v, v.pollLater()
		}
		return v, nil

	case tickMsg:
		if !v.complete {
			return v, v.fetch()
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			v.follow = false
			v.offset = min(v.offset+1, max(0, len(v.lines)-v.pageSize()))
		case "k", "up":
			v.follow = false
			v.offset = max(0, v.offset-1)
		case "g":
			v.follow = false
			v.offset = 0
		case "G":
			v.follow = true
			v.scrollToEnd()
		}
	}
	return v, nil
}

func (v *StepLogsView) pageSize() int {
	return max(v.height-6, 5)
}

func (v *StepLogsView) scrollToEnd() {
	v.offset = max(0, len(v.lines)-v.pageSize())
}

func (v *StepLogsView) View() string {
	var b strings.Builder
	status := "running…"
	if v.complete {
		status = "complete"
	}
	fmt.Fprintf(&b, "%s %s\n\n", TitleStyle.Render("  📋 Logs: "+v.stepName), DimStyle.Render("("+status+")"))

	if v.errMsg != "" {
		fmt.Fprintf(&b, "%s\n", ErrorStyle.Render("  "+v.errMsg))
	}
	if len(v.lines) == 0 {
		b.WriteString(DimStyle.Render("  (no output yet)"))
		return b.String()
	}

	end := min(v.offset+v.pageSize(), len(v.lines))
	for _, line := range v.lines[v.offset:end] {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	if end < len(v.lines) {
		fmt.Fprintf(&b, "%s\n", DimStyle.Render(fmt.Sprintf("  … %d more lines (j/G to scroll)", len(v.lines)-end)))
	}
	return b.String()
}

func (v *StepLogsView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *StepLogsView) ShortHelp() string { return "  j/k scroll  G follow  esc back" }
