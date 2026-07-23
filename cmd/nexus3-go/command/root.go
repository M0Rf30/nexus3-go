// Package command implements the nexus3-go Cobra command tree.
package command

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3"
	"github.com/spf13/cobra"
)

// Environment variable fallbacks for persistent flags.
const (
	envURL      = "NEXUS3_URL"
	envUsername = "NEXUS3_USERNAME"
	envPassword = "NEXUS3_PASSWORD"
)

// defaultTimeout bounds how long a single API request may take.
const defaultTimeout = 30 * time.Second

// newRootCmd builds the root nexus3-go command with every subcommand wired
// in explicitly, so the tree stays testable without package-level init().
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "nexus3-go",
		Short:         "Command-line client for Sonatype Nexus Repository Manager 3",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().String("url", os.Getenv(envURL),
		"Nexus Repository Manager base URL (env "+envURL+")")
	root.PersistentFlags().String("username", os.Getenv(envUsername),
		"Nexus username (env "+envUsername+")")
	root.PersistentFlags().String("password", os.Getenv(envPassword),
		"Nexus password (env "+envPassword+")")
	root.PersistentFlags().Duration("timeout", defaultTimeout,
		"Timeout applied to each API request")

	root.AddCommand(newStatusCmd())
	root.AddCommand(newRepositoriesCmd())
	root.AddCommand(newComponentsCmd())
	root.AddCommand(newVersionCmd())

	return root
}

// Execute runs the root command; main() prints any returned error.
func Execute() error {
	return newRootCmd().Execute()
}

// newClient builds a Nexus client from the root command's persistent flags.
func newClient(cmd *cobra.Command) (*nexus3.Client, error) {
	url, err := cmd.Flags().GetString("url")
	if err != nil {
		return nil, fmt.Errorf("reading --url flag: %w", err)
	}
	if url == "" {
		return nil, fmt.Errorf("nexus base URL is required (--url or %s)", envURL)
	}

	username, err := cmd.Flags().GetString("username")
	if err != nil {
		return nil, fmt.Errorf("reading --username flag: %w", err)
	}

	password, err := cmd.Flags().GetString("password")
	if err != nil {
		return nil, fmt.Errorf("reading --password flag: %w", err)
	}

	var opts []nexus3.Option
	if username != "" || password != "" {
		opts = append(opts, nexus3.WithBasicAuth(username, password))
	}

	return nexus3.New(url, opts...), nil
}

// commandContext returns a context bound by the --timeout persistent flag.
// The caller must invoke the returned cancel function.
func commandContext(cmd *cobra.Command) (context.Context, context.CancelFunc, error) {
	timeout, err := cmd.Flags().GetDuration("timeout")
	if err != nil {
		return nil, nil, fmt.Errorf("reading --timeout flag: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return ctx, cancel, nil
}
