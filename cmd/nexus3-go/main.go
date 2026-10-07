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
// One operation falls outside that coverage: uploading a component (POST
// /v1/components) needs multipart fields (apt.asset, yum.asset, raw.asset1,
// maven2.asset1, ...) that Nexus's own OpenAPI document does not declare, so
// Restish can never generate flags for them. "upload <kind>" is a small
// hand-written command for exactly that case — kinds: deb, rpm, raw,
// maven2, go, helm, npm, nuget, pypi, rubygems (every hosted format Nexus
// exposes through this endpoint except docker, which uses the separate
// Docker Registry HTTP API v2 instead). See cmd/nexus3-go/upload.go and
// pkg/nexus3.UploadDeb/UploadRpm/UploadRaw/UploadMaven2/UploadSimple.
//
//	NEXUS_USERNAME=admin NEXUS_PASSWORD=*** nexus3-go upload deb \
//	    --base-url http://localhost:8081 --repository apt-hosted --file pkg.deb
//	NEXUS_USERNAME=admin NEXUS_PASSWORD=*** nexus3-go upload maven2 \
//	    --base-url http://localhost:8081 --repository maven-hosted --file lib.jar \
//	    --group-id com.example --artifact-id lib --version 1.0
//
// For programmatic access from Go code, see the pkg/nexus3 library instead.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	restish "github.com/rest-sh/restish/v2"
)

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// run executes the CLI for args (including the program name) and returns the
// process exit code. "upload", "cleanup", "version" and "--version" are handled here;
// everything else goes to Restish. Ctrl-C or SIGTERM cancels in-flight
// uploads.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		switch args[1] {
		case "version", "--version":
			printVersion(stdout)
			return 0
		case "cleanup":
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := runCleanupContext(ctx, args[2:], stdout, stderr); err != nil {
				_, _ = fmt.Fprintln(stderr, err)
				return 1
			}

			return 0
		case "upload":
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := runUploadContext(ctx, args[2:], stdout, stderr); err != nil {
				_, _ = fmt.Fprintln(stderr, err)
				return 1
			}

			return 0
		}
	}

	cli := restish.New()
	cli.SetCommandName("nexus3-go")
	cli.SetVersion(buildVersion())

	if err := cli.Run(args); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}
