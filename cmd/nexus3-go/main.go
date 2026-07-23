// Command nexus3-go is a Restish-based CLI with total command coverage of
// any Sonatype Nexus Repository Manager 3 instance's REST API.
//
// Restish (github.com/rest-sh/restish) discovers the live OpenAPI/Swagger
// document a Nexus server publishes at /service/rest/swagger.json and
// generates one CLI command per operation, so coverage always matches
// whatever API surface that specific server version actually exposes.
//
// Typical usage:
//
//	nexus3-go api connect nexus http://localhost:8081 \
//	    --spec http://localhost:8081/service/rest/swagger.json
//	nexus3-go nexus --help
//	nexus3-go nexus list-repositories
//
// For programmatic access from Go code, see the pkg/nexus3 library instead.
package main

import (
	"fmt"
	"os"

	restish "github.com/rest-sh/restish/v2"
)

func main() {
	cli := restish.New()
	cli.SetCommandName("nexus3-go")

	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
