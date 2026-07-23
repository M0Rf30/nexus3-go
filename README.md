# nexus3-go

UNOFFICIAL Go client and CLI for Sonatype Nexus Repository Manager 3, built on the community-generated OpenAPI client.

[![CI](https://github.com/M0Rf30/nexus3-go/actions/workflows/ci.yml/badge.svg)](https://github.com/M0Rf30/nexus3-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/M0Rf30/nexus3-go.svg)](https://pkg.go.dev/github.com/M0Rf30/nexus3-go)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Overview

`nexus3-go` has two independent parts:

- **`pkg/nexus3`** — a small library wrapping the community-generated OpenAPI
  client [`github.com/sonatype-nexus-community/nexus-repo-api-client-go`](https://github.com/sonatype-nexus-community/nexus-repo-api-client-go)
  for use from Go programs. It configures the client, injects basic auth per
  request, and exposes a handful of convenience methods (`ListRepositories`,
  `SearchComponents`, `Status`, ...) plus an escape hatch to the full
  generated client for anything else.

- **`cmd/nexus3-go`** — a CLI with *total* command coverage of a Nexus
  instance's REST API, built by embedding [Restish](https://rest.sh)
  (`github.com/rest-sh/restish`). Restish discovers the live OpenAPI document
  a Nexus server publishes at `/service/rest/swagger.json` and generates one
  CLI command per operation — the full ~950-endpoint surface (repositories,
  components, assets, search, blobstores, security, tasks, staging, cleanup
  policies, and more), always in sync with whatever version that specific
  server actually runs, with no code generation or vendoring on our side.

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

Only a handful of convenience methods are wrapped (`ListRepositories`,
`SearchComponents`/`ListComponents`, `Status`). For anything else — blob
stores, security, tasks, staging, cleanup policies, and the rest of Nexus's
~950-endpoint REST surface — drop down to the generated client directly via
`Client.API()`, authenticating each call with `Client.AuthContext(ctx)`:

```go
api := client.API()
ctx := client.AuthContext(context.Background())

task, _, err := api.TasksAPI.GetTaskById(ctx, taskID).Execute()
```

## CLI usage

Connect once per Nexus instance (credentials and the discovered command set
are persisted under Restish's config directory):

```sh
nexus3-go api connect nexus http://localhost:8081 \
    --spec http://localhost:8081/service/rest/swagger.json
```

You'll be prompted for the Basic Auth username/password Nexus requires (or
pass them inline, e.g. `prompt.credentials.BasicAuth.username:admin`). Then
every discovered operation is available as a subcommand under the profile
name you chose (`nexus` above):

```sh
nexus3-go nexus --help
nexus3-go nexus get-all-repositories
nexus3-go nexus create-maven-hosted-repository 'name: releases, ...'
nexus3-go nexus get-all-repositories --help   # full flag/schema/example docs
```

See the [Restish docs](https://rest.sh/docs/) for output formatting
(`-o table`, `-f` shorthand filters), pagination, and profile management —
all of it applies unchanged since `nexus3-go` is a thin, Nexus-named build of
the stock Restish CLI.

## Uploading components

Nexus's own OpenAPI/swagger.json declares no request body for the component
upload endpoint (`POST /v1/components`), so Restish can never generate flags
for it — the CLI's generic `nexus3-go nexus ...` command tree cannot reach
it. A small hand-written `upload` command covers it instead, for every
hosted format Nexus exposes through that endpoint except docker (which uses
the separate Docker Registry HTTP API v2 — plain `docker push`):

```sh
export NEXUS_USERNAME=admin NEXUS_PASSWORD=admin123

nexus3-go upload deb --base-url http://localhost:8081 \
    --repository apt-hosted --file mypkg_1.0_amd64.deb
nexus3-go upload rpm --base-url http://localhost:8081 \
    --repository yum-hosted --file mypkg-1.0.el9.x86_64.rpm
nexus3-go upload raw --base-url http://localhost:8081 \
    --repository raw-hosted --directory docs --file notes.txt
nexus3-go upload maven2 --base-url http://localhost:8081 \
    --repository maven-hosted --file lib.jar \
    --group-id com.example --artifact-id lib --version 1.0
nexus3-go upload npm --base-url http://localhost:8081 \
    --repository npm-hosted --file mypkg-1.0.0.tgz   # also: go, helm, nuget, pypi, rubygems
```

`--base-url` also reads from `NEXUS_BASE_URL` if unset.

## Docker

Multi-arch (`linux/amd64`, `linux/arm64`) images are published to GHCR on
every tagged release:

```sh
docker run --rm ghcr.io/m0rf30/nexus3-go:latest --help
```

Pin to a specific version instead of `latest` for reproducible pulls
(e.g. `ghcr.io/m0rf30/nexus3-go:0.1.0` — goreleaser tags images with the bare
version, without the `v` prefix used for git tags).

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
