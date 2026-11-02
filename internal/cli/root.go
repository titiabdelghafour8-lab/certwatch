// Package cli implements the certwatch command-line interface.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/titiabdelghafour8/certwatch/internal/client"
	"github.com/titiabdelghafour8/certwatch/internal/version"
)

// ExitError carries a specific process exit code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

type app struct {
	out, errOut io.Writer

	flagURL    string
	flagKey    string
	configPath string
	jsonOut    bool

	cfg fileConfig
}

// apiURL resolves the server URL: flag > env > config file > build default.
func (a *app) apiURL() string {
	for _, v := range []string{a.flagURL, os.Getenv("CERTWATCH_API_URL"), a.cfg.APIURL} {
		if v != "" {
			return v
		}
	}
	return version.DefaultAPIURL
}

func (a *app) apiKey() string {
	for _, v := range []string{a.flagKey, os.Getenv("CERTWATCH_API_KEY"), a.cfg.APIKey} {
		if v != "" {
			return v
		}
	}
	return ""
}

func (a *app) anonClient() *client.Client { return client.New(a.apiURL(), "") }

func (a *app) client() (*client.Client, error) {
	key := a.apiKey()
	if key == "" {
		return nil, errors.New("not logged in: run `certwatch signup --email you@example.com` or `certwatch login`")
	}
	return client.New(a.apiURL(), key), nil
}

// NewRootCmd builds the command tree.
func NewRootCmd(out, errOut io.Writer) *cobra.Command {
	a := &app{out: out, errOut: errOut}
	root := &cobra.Command{
		Use:   "certwatch",
		Short: "Never let a TLS certificate or domain expire again",
		Long: `certwatch monitors TLS certificates and domain registrations and alerts you
before they expire, by email, Slack, Discord or webhook.

Check anything right now without an account:
  certwatch check example.com

Or create a free account and let certwatch watch continuously:
  certwatch signup --email you@example.com
  certwatch add example.com
  certwatch list`,
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if a.configPath == "" {
				a.configPath = defaultConfigPath()
			}
			cfg, err := loadConfig(a.configPath)
			if err != nil {
				return fmt.Errorf("reading %s: %w", a.configPath, err)
			}
			a.cfg = cfg
			return nil
		},
	}
	root.SetOut(out)
	root.SetErr(errOut)

	pf := root.PersistentFlags()
	pf.StringVar(&a.flagURL, "api-url", "", "API server URL (env CERTWATCH_API_URL)")
	pf.StringVar(&a.flagKey, "api-key", "", "API key (env CERTWATCH_API_KEY)")
	pf.StringVar(&a.configPath, "config", "", "config file path (default "+defaultConfigPath()+")")
	pf.BoolVar(&a.jsonOut, "json", false, "print machine-readable JSON")

	root.AddGroup(
		&cobra.Group{ID: "local", Title: "Local checks (no account needed):"},
		&cobra.Group{ID: "monitor", Title: "Monitoring:"},
		&cobra.Group{ID: "account", Title: "Account:"},
	)
	for _, c := range []*cobra.Command{a.checkCmd()} {
		c.GroupID = "local"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{a.addCmd(), a.listCmd(), a.showCmd(), a.removeCmd(), a.recheckCmd(),
		a.pauseCmd(true), a.pauseCmd(false), a.channelsCmd()} {
		c.GroupID = "monitor"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{a.signupCmd(), a.loginCmd(), a.logoutCmd(), a.whoamiCmd(), a.recoverCmd(),
		a.keysCmd(), a.upgradeCmd(), a.billingCmd()} {
		c.GroupID = "account"
		root.AddCommand(c)
	}
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "certwatch", version.String())
		},
	})
	return root
}

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	root := NewRootCmd(os.Stdout, os.Stderr)
	if err := root.Execute(); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			if exitErr.Err != nil {
				fmt.Fprintln(os.Stderr, "error:", exitErr.Err)
			}
			return exitErr.Code
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}
