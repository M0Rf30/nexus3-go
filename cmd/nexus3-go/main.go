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
	"fmt"
	"os"

	restish "github.com/rest-sh/restish/v2"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "upload" {
		if err := runUpload(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	cli := restish.New()
	cli.SetCommandName("nexus3-go")

	if err := cli.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
