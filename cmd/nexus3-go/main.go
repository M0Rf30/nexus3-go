// Command nexus3-go is a CLI client for Sonatype Nexus Repository Manager 3.
package main

import (
	"fmt"
	"os"

	"github.com/M0Rf30/nexus3-go/cmd/nexus3-go/command"
)

func main() {
	if err := command.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
