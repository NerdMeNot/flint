package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// FleetView shows the machine fleet — status, shape, price, work done — and
// lets the operator drain (d) the selected machine.
type FleetView struct {
	client   *Client
	machines []Machine
	cursor   int
	loading  bool
	errMsg   string
	notice   string
	width    int
	height   int
}

func NewFleetView(client *Client) *FleetView {
	return &FleetView{client: client, loading: true}
}

func (v *FleetView) Init() tea.Cmd { return v.load() }

func (v *FleetView) load() tea.Cmd {
	return func() tea.Msg {
		machines, err := v.client.ListMachines(context.Background())
		return machinesLoadedMsg{machines: machines, err: err}
	}
}

func (v *FleetView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg := msg.(type) {
	case machinesLoadedMsg:
		v.loading = false
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			return v, nil
		}
		v.errMsg = ""
		v.machines = msg.machines
		if v.cursor >= len(v.machines) {
			v.cursor = max(0, len(v.machines)-1)
		}
		return v, nil

	case machineDrainedMsg:
		if msg.err != nil {
			v.errMsg = msg.err.Error()
			return v, nil
		}
		v.notice = "✓ draining"
		return v, v.load()

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if v.cursor < len(v.machines)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "d":
			if m := v.selected(); m != nil && (m.Status == "idle" || m.Status == "busy") {
				return v, v.drain(m.ID)
			}
		case "R":
			v.loading = true
			return v, v.load()
		}
	}
	return v, nil
}

func (v *FleetView) selected() *Machine {
	if v.cursor < len(v.machines) {
		return &v.machines[v.cursor]
	}
	return nil
}

func (v *FleetView) drain(id string) tea.Cmd {
	return func() tea.Msg {
		return machineDrainedMsg{err: v.client.DrainMachine(context.Background(), id)}
	}
}

func (v *FleetView) View() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("  ⛭ Fleet") + "\n\n")

	switch {
	case v.loading:
		b.WriteString(DimStyle.Render("  loading…"))
	case v.errMsg != "":
		b.WriteString(ErrorStyle.Render("  " + v.errMsg))
	case len(v.machines) == 0:
		b.WriteString(DimStyle.Render("  No machines. Join one with a pool join token."))
	default:
		for i, m := range v.machines {
			cursor := "  "
			if i == v.cursor {
				cursor = "▎ "
			}
			fmt.Fprintf(&b, "%s%-10s %-14s %-12s %s\n",
				cursor, machineStatusGlyph(m.Status)+" "+m.Status, machineName(m),
				machineShape(m), DimStyle.Render(machineEconomics(m)))
			if i == v.cursor && m.DrainReason != nil {
				fmt.Fprintf(&b, "     %s\n", DimStyle.Render("draining: "+*m.DrainReason))
			}
		}
	}
	if v.notice != "" {
		b.WriteString("\n" + DimStyle.Render("  "+v.notice))
	}
	return b.String()
}

func (v *FleetView) SetSize(w, h int)  { v.width = w; v.height = h }
func (v *FleetView) ShortHelp() string { return "  d drain  R refresh  esc back" }

func machineName(m Machine) string {
	if m.Hostname != nil && *m.Hostname != "" {
		return *m.Hostname
	}
	return m.ID[:8]
}

func machineShape(m Machine) string {
	if m.InstanceType != nil && *m.InstanceType != "" {
		s := *m.InstanceType
		if m.CapacityType != nil && *m.CapacityType == "spot" {
			s += " spot"
		}
		return s
	}
	return fmt.Sprintf("%dc/%dG %s", m.CPUMillis/1000, m.MemoryMB/1024, m.Arch)
}

func machineEconomics(m Machine) string {
	parts := []string{fmt.Sprintf("%d steps", m.StepsCompleted)}
	if m.PricePerHourUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.3f/hr", *m.PricePerHourUSD))
	}
	if m.CostToDateUSD != nil {
		parts = append(parts, fmt.Sprintf("$%.2f total", *m.CostToDateUSD))
	}
	return strings.Join(parts, "  ")
}

func machineStatusGlyph(status string) string {
	switch status {
	case "idle":
		return "●"
	case "busy":
		return "◉"
	case "provisioning", "requested":
		return "◌"
	case "draining", "terminating":
		return "◍"
	case "failed", "lost":
		return "✗"
	default:
		return "○"
	}
}
