package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/titiabdelghafour8-lab/certwatch/internal/model"
)

func (a *app) channelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "channels",
		Aliases: []string{"channel", "ch"},
		Short:   "Manage where alerts are sent (email, Slack, Discord, webhook)",
		Long: `Manage notification channels. Without any channel, alerts go to your
account email (when the server has email configured).`,
	}

	var name string
	add := &cobra.Command{
		Use:   "add <email|slack|discord|webhook> <target>",
		Short: "Add a notification channel",
		Example: `  certwatch channels add slack https://hooks.slack.com/services/T000/B000/XXXX
  certwatch channels add discord https://discord.com/api/webhooks/123/abc
  certwatch channels add webhook https://example.com/certwatch-hook
  certwatch channels add email you@example.com`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			ch, err := c.CreateChannel(cmd.Context(), model.CreateChannelRequest{Kind: model.ChannelKind(args[0]), Target: args[1], Name: name})
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, ch)
			}
			fmt.Fprintf(a.out, "added %s channel %d. Send a test with: certwatch channels test %d\n", ch.Kind, ch.ID, ch.ID)
			return nil
		},
	}
	add.Flags().StringVar(&name, "name", "", "optional display name")

	list := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List notification channels",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			chs, err := c.ListChannels(cmd.Context())
			if err != nil {
				return err
			}
			if a.jsonOut {
				return printJSON(a.out, chs)
			}
			if len(chs) == 0 {
				fmt.Fprintln(a.out, "No channels. Alerts go to your account email if the server supports it.")
				return nil
			}
			t := newTable(a.out)
			fmt.Fprintln(t, "ID\tKIND\tNAME\tTARGET")
			for _, ch := range chs {
				fmt.Fprintf(t, "%d\t%s\t%s\t%s\n", ch.ID, ch.Kind, ch.Name, ch.Target)
			}
			return t.Flush()
		},
	}

	rm := &cobra.Command{
		Use:     "rm <id>",
		Aliases: []string{"remove", "delete"},
		Short:   "Remove a notification channel",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID(args[0])
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			if err := c.DeleteChannel(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "removed channel %d\n", id)
			return nil
		},
	}

	test := &cobra.Command{
		Use:   "test <id>",
		Short: "Send a test notification",
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
			if err := c.TestChannel(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "test notification delivered ✔")
			return nil
		},
	}

	cmd.AddCommand(add, list, rm, test)
	return cmd
}
