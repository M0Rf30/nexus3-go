package command

import (
	"fmt"
	"text/tabwriter"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
	"github.com/spf13/cobra"
)

// maxSearchPages caps how many continuation pages components search will
// follow. Nexus paginates via an opaque continuation token; a misbehaving or
// misconfigured server could in principle keep returning a non-empty token
// forever, so this bounds the loop instead of trusting the server to
// terminate it.
const maxSearchPages = 10

// newComponentsCmd builds the `components` parent subcommand.
func newComponentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "components",
		Short: "Inspect Nexus components",
	}

	cmd.AddCommand(newComponentsSearchCmd())

	return cmd
}

// newComponentsSearchCmd builds the `components search` subcommand.
func newComponentsSearchCmd() *cobra.Command {
	var repository, query string

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search components in a repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if repository == "" {
				return fmt.Errorf("--repository is required")
			}

			client, err := newClient(cmd)
			if err != nil {
				return err
			}

			ctx, cancel, err := commandContext(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "GROUP\tNAME\tVERSION")

			components, token, err := client.SearchComponents(ctx, repository, query)
			if err != nil {
				return fmt.Errorf("searching components: %w", err)
			}
			printComponents(w, components)

			for page := 1; token != "" && page < maxSearchPages; page++ {
				components, token, err = client.ListComponents(ctx, repository, token)
				if err != nil {
					return fmt.Errorf("listing components page %d: %w", page+1, err)
				}
				printComponents(w, components)
			}

			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&repository, "repository", "", "Repository to search (required)")
	cmd.Flags().StringVar(&query, "query", "", "Search query")

	return cmd
}

func printComponents(w *tabwriter.Writer, components []v3.ComponentXO) {
	for _, c := range components {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", c.GetGroup(), c.GetName(), c.GetVersion())
	}
}
