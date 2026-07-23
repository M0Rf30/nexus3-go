package command

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newStatusCmd builds the `status` subcommand.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check connectivity to the Nexus Repository Manager instance",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := newClient(cmd)
			if err != nil {
				return err
			}

			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			if err := client.Status(ctx); err != nil {
				return fmt.Errorf("nexus status check failed: %w", err)
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "OK")
			return nil
		},
	}
}
