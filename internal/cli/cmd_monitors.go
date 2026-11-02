package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/titiabdelghafour8/certwatch/internal/client"
	"github.com/titiabdelghafour8/certwatch/internal/model"
)

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q (see `certwatch list`)", s)
	}
	return id, nil
}

func (a *app) addCmd() *cobra.Command {
	var (
		tlsOnly, domOnly, noCheck bool
		label, warn               string
	)
	cmd := &cobra.Command{
		Use:   "add <host>...",
		Short: "Start monitoring hosts (TLS certificate + domain registration)",
		Example: `  certwatch add example.com
  certwatch add shop.example.com:8443 --tls-only --label "checkout"
  certwatch add example.com --warn-days 60,30,7`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			if tlsOnly && domOnly {
				return errors.New("--tls-only and --domain-only are mutually exclusive")
			}
			warnDays, err := parseIntList(warn)
			if err != nil {
				return err
			}
			var kinds []model.MonitorKind
			if !domOnly {
				kinds = append(kinds, model.KindTLS)
			}
			if !tlsOnly {
				kinds = append(kinds, model.KindDomain)
			}

			var created []model.Monitor
			var failures int
			for _, host := range args {
				for _, kind := range kinds {
					m, err := c.CreateMonitor(cmd.Context(), model.CreateMonitorRequest{Kind: kind, Target: host, Label: label, WarnDays: warnDays})
					var apiErr *client.APIError
					switch {
					case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
						fmt.Fprintf(a.errOut, "skip: %s\n", apiErr.Message)
						continue
					case errors.As(err, &apiErr) && apiErr.Status == http.StatusPaymentRequired:
						return err
					case err != nil:
						failures++
						fmt.Fprintf(a.errOut, "error adding %s monitor for %s: %v\n", kind, host, err)
						continue
					}
					if !noCheck {
						if checked, err := c.CheckMonitor(cmd.Context(), m.ID); err == nil {
							m = checked
						}
					}
					created = append(created, m)
				}
			}
			if a.jsonOut {
				return printJSON(a.out, created)
			}
			if len(created) > 0 {
				printMonitors(a.out, created)
			}
			if failures > 0 {
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&tlsOnly, "tls-only", false, "only monitor the TLS certificate")
	f.BoolVar(&domOnly, "domain-only", false, "only monitor the domain registration")
	f.BoolVar(&noCheck, "no-check", false, "do not run the first check immediately")
	f.StringVar(&label, "label", "", "free-form label shown in alerts")
	f.StringVar(&warn, "warn-days", "", "comma-separated alert thresholds in days (default 30,14,7,3,1)")
	return cmd
}

func (a *app) listCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List monitors, soonest expiry first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			ms, err := c.ListMonitors(cmd.Context())
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, ms)
			}
			if len(ms) == 0 {
				fmt.Fprintln(a.out, "No monitors yet. Add one with: certwatch add example.com")
				return nil
			}
			printMonitors(a.out, ms)
			return nil
		},
	}
}

func (a *app) showCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show one monitor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			m, err := c.GetMonitor(cmd.Context(), id)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, m)
			}
			t := newTable(a.out)
			fmt.Fprintf(t, "ID:\t%d\nKind:\t%s\nTarget:\t%s\nLabel:\t%s\nStatus:\t%s\nExpires:\t%s\nDays left:\t%s\n",
				m.ID, m.Kind, m.Target, m.Label, colorStatus(m.Status, colorEnabled(a.out)), fmtDate(m.ExpiresAt), fmtDays(m.DaysLeft))
			fmt.Fprintf(t, "Issuer:\t%s\nAlert at:\t%v days\nPaused:\t%v\nLast checked:\t%s\nNext check:\t%s\n",
				m.Issuer, m.WarnDays, m.Paused, fmtTime(m.LastCheckedAt), m.NextCheckAt.Local().Format("2006-01-02 15:04"))
			if m.LastError != "" {
				fmt.Fprintf(t, "Note:\t%s\n", m.LastError)
			}
			return t.Flush()
		},
	}
}

func (a *app) removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "rm <id>...",
		Aliases: []string{"remove", "delete"},
		Short:   "Stop monitoring",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			for _, s := range args {
				id, err := parseID(s)
				if err != nil {
					return err
				}
				if err := c.DeleteMonitor(cmd.Context(), id); err != nil {
					return err
				}
				fmt.Fprintf(a.out, "removed monitor %d\n", id)
			}
			return nil
		},
	}
}

func (a *app) recheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "recheck <id>",
		Short: "Run a monitor's check now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			m, err := c.CheckMonitor(cmd.Context(), id)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, m)
			}
			printMonitors(a.out, []model.Monitor{m})
			return nil
		},
	}
}

func (a *app) pauseCmd(pause bool) *cobra.Command {
	use, short := "resume <id>", "Resume a paused monitor"
	if pause {
		use, short = "pause <id>", "Pause a monitor without deleting it"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			m, err := c.SetPaused(cmd.Context(), id, pause)
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, m)
			}
			state := "resumed"
			if m.Paused {
				state = "paused"
			}
			fmt.Fprintf(a.out, "monitor %d %s\n", m.ID, state)
			return nil
		},
	}
}
