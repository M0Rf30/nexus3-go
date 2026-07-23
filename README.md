# nexus3-go

UNOFFICIAL Go client and CLI for Sonatype Nexus Repository Manager 3, built on the community-generated OpenAPI client.

[![CI](https://github.com/M0Rf30/nexus3-go/actions/workflows/ci.yml/badge.svg)](https://github.com/M0Rf30/nexus3-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/M0Rf30/nexus3-go.svg)](https://pkg.go.dev/github.com/M0Rf30/nexus3-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Overview

`nexus3-go` is a thin wrapper around
[`github.com/sonatype-nexus-community/nexus-repo-api-client-go`](https://github.com/sonatype-nexus-community/nexus-repo-api-client-go),
a community-generated OpenAPI client that already covers the full Nexus
Repository Manager 3 REST surface (repositories, components, assets, search,
blobstores, security, tasks, and more). This project does not regenerate or
reimplement that client — it wraps it with:

- a small `pkg/nexus3` library that configures the generated client, injects
  basic auth per request, and exposes a handful of convenience methods
  (`ListRepositories`, `SearchComponents`, `Status`, ...), and
- a `nexus3-go` CLI built on [Cobra](https://github.com/spf13/cobra) for
  common day-to-day operations against a Nexus instance.

## Installation

Library:

```sh
go get github.com/M0Rf30/nexus3-go
```

CLI:

```sh
go install github.com/M0Rf30/nexus3-go/cmd/nexus3-go@latest
```

## Library usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3"
)

func main() {
	client := nexus3.New("http://localhost:8081",
		nexus3.WithBasicAuth("admin", "admin123"))

	repos, err := client.ListRepositories(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	for _, repo := range repos {
		fmt.Println(repo.Name)
	}
}
```

## CLI usage

`--url`, `--username`, and `--password` flags fall back to the
`NEXUS3_URL`, `NEXUS3_USERNAME`, and `NEXUS3_PASSWORD` environment variables
respectively, so credentials don't need to be passed on every invocation:

```sh
export NEXUS3_URL=http://localhost:8081
export NEXUS3_USERNAME=admin
export NEXUS3_PASSWORD=admin123

nexus3-go status

nexus3-go repositories list

nexus3-go components search --repository maven-releases --query foo
```

Equivalently, flags can be passed explicitly instead of using environment
variables:

```sh
nexus3-go --url http://localhost:8081 --username admin --password admin123 status
```

## Development

```sh
make build   # build ./bin/nexus3-go
make test    # go test -v ./...
make lint    # golangci-lint run
```

See `make help` for the full target list.

## License

[MIT](LICENSE)

## Disclaimer

This project is not affiliated with, endorsed by, or supported by Sonatype.
"Nexus" and "Sonatype" are trademarks of Sonatype, Inc.
