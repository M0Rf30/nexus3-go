package command

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newRepositoriesCmd builds the `repositories` parent subcommand.
func newRepositoriesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repositories",
		Short: "Inspect Nexus repositories",
	}

	cmd.AddCommand(newRepositoriesListCmd())

	return cmd
}

// newRepositoriesListCmd builds the `repositories list` subcommand.
func newRepositoriesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all repositories",
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

			repos, err := client.ListRepositories(ctx)
			if err != nil {
				return fmt.Errorf("listing repositories: %w", err)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tFORMAT\tTYPE\tURL")
			for _, repo := range repos {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", repo.GetName(), repo.GetFormat(), repo.GetType(), repo.GetUrl())
			}

			return w.Flush()
		},
	}
}
