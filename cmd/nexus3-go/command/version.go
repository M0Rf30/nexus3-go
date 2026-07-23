package command

import (
	"fmt"

	"github.com/M0Rf30/nexus3-go/pkg/buildinfo"
	"github.com/spf13/cobra"
)

// newVersionCmd builds the `version` subcommand.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the nexus3-go version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), buildinfo.String())
			return nil
		},
	}
}
