package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/titiabdelghafour8-lab/certwatch/internal/client"
	"github.com/titiabdelghafour8-lab/certwatch/internal/plans"
)

func (a *app) saveKey(key string) error {
	a.cfg.APIKey = key
	if a.flagURL != "" {
		a.cfg.APIURL = a.flagURL
	}
	if err := saveConfig(a.configPath, a.cfg); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	return nil
}

func (a *app) signupCmd() *cobra.Command {
	var email string
	cmd := &cobra.Command{
		Use:   "signup",
		Short: "Create a free account",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}
			res, err := a.anonClient().Signup(cmd.Context(), email)
			if err != nil {
				return err
			}
			if res.APIKey == "" {
				fmt.Fprintln(a.out, res.Message)
				return nil
			}
			if err := a.saveKey(res.APIKey); err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, res)
			}
			fmt.Fprintf(a.out, "Account created for %s and logged in.\n", email)
			fmt.Fprintf(a.out, "Your API key (shown once, saved to %s):\n\n  %s\n\n", a.configPath, res.APIKey)
			fmt.Fprintln(a.out, "Next: certwatch add example.com")
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "your email address")
	return cmd
}

func (a *app) loginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save an API key on this machine",
		Long:  "Save an API key. Pass it with --api-key, or paste it when prompted.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			key := a.flagKey
			if key == "" {
				fmt.Fprint(a.errOut, "Paste your API key: ")
				line, err := bufio.NewReader(os.Stdin).ReadString('\n')
				if err != nil && line == "" {
					return errors.New("no API key provided")
				}
				key = strings.TrimSpace(line)
			}
			me, err := client.New(a.apiURL(), key).Me(cmd.Context())
			if err != nil {
				return fmt.Errorf("could not log in: %w", err)
			}
			if err := a.saveKey(key); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Logged in as %s (%s plan).\n", me.User.Email, me.Plan.Name)
			return nil
		},
	}
	return cmd
}

func (a *app) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved API key from this machine",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			a.cfg.APIKey = ""
			if err := saveConfig(a.configPath, a.cfg); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "Logged out.")
			return nil
		},
	}
}

func (a *app) recoverCmd() *cobra.Command {
	var email string
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Email yourself a new API key",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if email == "" {
				return errors.New("--email is required")
			}
			res, err := a.anonClient().Recover(cmd.Context(), email)
			if err != nil {
				return err
			}
			fmt.Fprintln(a.out, res.Message)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "your account email")
	return cmd
}

func limitStr(n int) string {
	if n == plans.Unlimited {
		return "unlimited"
	}
	return fmt.Sprint(n)
}

func (a *app) whoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show your account, plan and usage",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			me, err := c.Me(cmd.Context())
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, me)
			}
			t := newTable(a.out)
			fmt.Fprintf(t, "Email:\t%s\nPlan:\t%s\nMonitors:\t%d / %s\nChannels:\t%d / %s\nServer:\t%s\n",
				me.User.Email, me.Plan.Name, me.Usage.Monitors, limitStr(me.Plan.MaxMonitors),
				me.Usage.Channels, limitStr(me.Plan.MaxChannels), a.apiURL())
			return t.Flush()
		},
	}
}

func (a *app) keysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "keys", Short: "Manage API keys (e.g. one per CI system)"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List active API keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			keys, err := c.ListKeys(cmd.Context())
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, keys)
			}
			t := newTable(a.out)
			fmt.Fprintln(t, "ID\tNAME\tPREFIX\tCREATED\tLAST USED")
			for _, k := range keys {
				fmt.Fprintf(t, "%d\t%s\t%s…\t%s\t%s\n", k.ID, k.Name, k.Prefix, k.CreatedAt.Format("2006-01-02"), fmtTime(k.LastUsedAt))
			}
			return t.Flush()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Create a new API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			res, err := c.CreateKey(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, res)
			}
			fmt.Fprintf(a.out, "Created key %d (%s). It is shown only once:\n\n  %s\n", res.Key.ID, res.Key.Name, res.Secret)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API key",
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
			if err := c.RevokeKey(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "revoked key %d\n", id)
			return nil
		},
	})
	return cmd
}

func (a *app) upgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "upgrade [pro|agency]",
		Short:     "Upgrade your plan (opens Stripe Checkout)",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{plans.Pro.Name, plans.Agency.Name},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				t := newTable(a.out)
				fmt.Fprintln(t, "PLAN\tMONITORS\tCHANNELS")
				for _, p := range append([]plans.Plan{plans.Free}, plans.Paid...) {
					fmt.Fprintf(t, "%s\t%s\t%s\n", p.Name, limitStr(p.MaxMonitors), limitStr(p.MaxChannels))
				}
				_ = t.Flush()
				fmt.Fprintln(a.out, "\nRun: certwatch upgrade pro")
				return nil
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			url, err := c.Checkout(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Complete your upgrade in the browser:\n\n  %s\n", url)
			openBrowser(url)
			return nil
		},
	}
}

func (a *app) billingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "billing",
		Short: "Manage your subscription and invoices (opens Stripe portal)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			url, err := c.Portal(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Manage your subscription here:\n\n  %s\n", url)
			openBrowser(url)
			return nil
		},
	}
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04")
}
