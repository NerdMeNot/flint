package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/NerdMeNot/flint/internal/cliauth"
	"github.com/NerdMeNot/flint/internal/tui"
)

// fleet.go — operator verbs for the machine fleet. `flint fleet` opens the
// interactive fleet screen; `ls` and `drain` are the scriptable forms.

func fleetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fleet",
		Short: "Inspect and manage the machine fleet",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := fleetClient()
			if err != nil {
				return err
			}
			app := tui.NewApp(client, tui.StartOption{Fleet: true})
			p := tea.NewProgram(app, tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
	cmd.AddCommand(fleetLsCmd(), fleetDrainCmd())
	return cmd
}

func fleetLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List machines (status, shape, economics)",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := fleetClient()
			if err != nil {
				return err
			}
			machines, err := client.ListMachines(cmd.Context())
			if err != nil {
				return err
			}
			if len(machines) == 0 {
				fmt.Println("No machines. Join one with a pool join token.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATUS\tNAME\tSHAPE\tPRICE\tSTEPS\tCOST")
			for _, m := range machines {
				name := m.ID[:8]
				if m.Hostname != nil && *m.Hostname != "" {
					name = *m.Hostname
				}
				shape := fmt.Sprintf("%dc/%dG %s", m.CPUMillis/1000, m.MemoryMB/1024, m.Arch)
				if m.InstanceType != nil && *m.InstanceType != "" {
					shape = *m.InstanceType
					if m.CapacityType != nil && *m.CapacityType == "spot" {
						shape += " (spot)"
					}
				}
				price, cost := "-", "-"
				if m.PricePerHourUSD != nil {
					price = fmt.Sprintf("$%.3f/hr", *m.PricePerHourUSD)
				}
				if m.CostToDateUSD != nil {
					cost = fmt.Sprintf("$%.2f", *m.CostToDateUSD)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
					m.ID[:8], m.Status, name, shape, price, m.StepsCompleted, cost)
			}
			return w.Flush()
		},
	}
}

func fleetDrainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "drain <machine-id>",
		Short: "Ask a machine to finish its work and stop claiming",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := fleetClient()
			if err != nil {
				return err
			}
			id := args[0]
			// Accept the short ID form `fleet ls` prints.
			if len(id) < 36 {
				machines, err := client.ListMachines(cmd.Context())
				if err != nil {
					return err
				}
				for _, m := range machines {
					if strings.HasPrefix(m.ID, id) {
						id = m.ID
						break
					}
				}
			}
			if err := client.DrainMachine(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Printf("✓ %s draining\n", id[:8])
			return nil
		},
	}
}

// fleetClient builds an authenticated API client the same way the TUI does.
func fleetClient() (*tui.Client, error) {
	if creds, err := cliauth.Load(); err == nil {
		if serverURL != "" {
			creds.ServerURL = strings.TrimRight(serverURL, "/")
		}
		return tui.NewAuthedClient(creds), nil
	}
	base := serverURL
	if base == "" {
		base = os.Getenv("FLINT_SERVER_URL")
	}
	if base == "" {
		return nil, fmt.Errorf("not logged in — run `flint login` (or pass --server for a demo server)")
	}
	return tui.NewClient(base), nil
}
