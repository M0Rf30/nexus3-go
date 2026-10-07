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

Beyond the generated commands, the CLI ships two hand-written commands:
`upload` (multipart component upload) and `cleanup` (client-side retention
policies, see [Cleanup](#cleanup)).

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
`SearchComponents`/`SearchComponentsPage`, `ListComponents`, `Status`). For
anything else — blob stores, security, tasks, staging, cleanup policies, and
the rest of Nexus's ~950-endpoint REST surface — drop down to the generated
client directly via `Client.API()`, authenticating each call with
`Client.AuthContext(ctx)`:

```go
api := client.API()
ctx := client.AuthContext(context.Background())

task, _, err := api.TasksAPI.GetTaskById(ctx, taskID).Execute()
```

### Pagination

`AllComponents` and `AllSearchResults` return Go 1.23 range-over-func
iterators that follow continuation tokens for you:

```go
for comp, err := range client.AllSearchResults(ctx, "maven-releases", "commons-*") {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(comp.GetName(), comp.GetVersion())
}
```

To page manually, feed the token from `SearchComponents` back into
`SearchComponentsPage` (same repository and query), or from one
`ListComponents` call into the next. Search and list tokens are not
interchangeable.

### Errors

When Nexus answers with a non-2xx status, the returned error wraps an
`*nexus3.APIError` carrying the status code and the server's response body,
which usually holds the real reason:

```go
var apiErr *nexus3.APIError
if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
	log.Printf("rejected by Nexus: %s", apiErr.Body)
}
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

Run `nexus3-go upload --help` for the full kind list, or
`nexus3-go upload <kind> --help` for that kind's flags. Ctrl-C (or SIGTERM)
cancels in-flight uploads, and `--timeout 5m` bounds the whole command;
files that never started are reported as cancelled.

### Multiple files

`--file` is repeatable, and trailing positional arguments are treated as
files too — mix and match whichever's convenient. Any value containing a
glob metacharacter (`*`, `?`, `[`) is expanded with `filepath.Glob`; quote
it so your shell passes the pattern through to `nexus3-go` instead of
expanding it itself:

```sh
nexus3-go upload deb --base-url http://localhost:8081 \
    --repository apt-hosted --file 'dist/*.deb'

nexus3-go upload rpm --base-url http://localhost:8081 \
    --repository yum-hosted \
    --file build/mypkg-1.0.el9.x86_64.rpm --file build/mypkg-1.0.el9.noarch.rpm
```

For every kind except maven2 (which packs its assets into a single request
— see below), each matched file becomes its own upload request, and up to
`--concurrency` (default `4`) run in parallel; the flag has no effect on
maven2. A failure on one file never aborts the rest: every file is
attempted regardless of earlier failures, and if any fail the command
exits non-zero after reporting how many succeeded (`uploaded N/M`) followed
by each failing path and its error.

### Maven2 multi-asset components

A single Maven2 component can bundle more than one asset — the jar, its
POM, a `-sources` jar — as long as they share one group/artifact/version
and go up together in one `POST /v1/components` request. Nexus's API caps
that request at three assets (`asset1`..`asset3`), which is a limit of the
Nexus API itself, not of this CLI — so `--asset` may be repeated at most
three times.

Once you have more than one asset, use the repeatable
`--asset path[:extension[:classifier]]` flag instead of `--file`:

```sh
nexus3-go upload maven2 --base-url http://localhost:8081 \
    --repository maven-hosted \
    --group-id com.example --artifact-id lib --version 1.0 \
    --asset lib.jar \
    --asset lib.pom:pom \
    --asset lib-sources.jar:jar:sources
```

Extension and classifier travel inside each `--asset` value rather than as
separate, index-aligned `--extension`/`--classifier` list flags: with
parallel arrays, a dropped or reordered entry silently attaches the wrong
metadata to the wrong file, and nothing catches it. Keeping
`path:extension:classifier` together as one token makes that class of bug
structurally impossible — there is no index to misalign.

`--extension` and `--classifier` still exist as plain scalar strings, but
only apply to the single-asset `--file` form above. `--file` and `--asset`
are mutually exclusive for maven2 — combining them is an error.

## Cleanup

Nexus Community Edition has no "retain N versions" cleanup policy: that
feature is Pro-only (and requires PostgreSQL). `nexus3-go cleanup` replaces it
client-side. It lists components through the REST API, decides what to delete
from a YAML policy file, and deletes them. It is a port of zextras'
[JFrog cleanup](https://github.com/zextras/infra-jfrog-cleanup); a ready-made
translation lives in [`examples/zextras-cleanup.yaml`](examples/zextras-cleanup.yaml).

```yaml
policies:
  - name: rc
    repositories: [ubuntu-rc-jammy, rhel9-rc]
    keepLatest: 3
    keepDays: 14
  - name: maven-snapshot
    repositories: [maven-snapshots]
    releaseType: prerelease
    olderThan: 90d
```

### Policy reference

| Key | Meaning |
|---|---|
| `name` | Required policy name, shown in the plan. |
| `repositories` | Required, at least one repository. |
| `keepLatest` | Keep the newest N versions per repository+group+name (0 = disabled). |
| `keepDays` | Also keep anything younger than N days (0 = today only; unset = disabled). Union with `keepLatest`. |
| `olderThan` | Delete components older than `90d` / `90` days (0 = disabled). |
| `releaseType` | `any` (default), `release` or `prerelease`; components outside it are never touched and do not count toward `keepLatest`. |
| `ageFrom` | `versionTimestamp` (default: timestamp embedded in the version, then blob creation time) or `uploaded`. |
| `versionOrder` | `auto` (default, by repository format) or `debian`, `rpm`, `maven`, `semver`, `lexical`. |
| `includeNames`, `excludeNames` | Regexes on the component name. |
| `protectVersions` | Regexes on versions that are never deleted. |

At least one of `keepLatest` or `olderThan` is required. With both, a component
is deleted only if it is outside the newest N **and** older than `olderThan`.
A component matched by several policies is planned once (first policy wins).
Migrated content has a blob creation date equal to the migration date, which is
why age defaults to the build timestamp embedded in the version.

### Workflow

```sh
export NEXUS_BASE_URL=https://nexus.example.com NEXUS_USERNAME=ci NEXUS_PASSWORD=***

nexus3-go cleanup --policy cleanup.yaml                      # dry-run: print the plan only
nexus3-go cleanup --policy cleanup.yaml --output json        # machine-readable plan
nexus3-go cleanup --policy cleanup.yaml --apply              # delete the planned components
nexus3-go cleanup --policy cleanup.yaml --apply --compact    # ...then run every blobstore.compact task
```

Deleting a component only marks blobs as deleted; disk space is reclaimed by a
"Compact blob store" task, which `--compact` triggers (a warning is printed if
none is configured). The command exits 1 if any delete fails.

### Request budget

Some CE deployments sit behind rate or request limits. `--max-requests N`
caps the work: listing counts one request per 100 components (minimum one per
repository) and each delete counts one. Planned deletes beyond the budget are
reported as failed with a request-budget error and can be picked up by the next
run. The plan prints its estimated request count.

### Jenkins

Mirrors the JFrog Jenkinsfile: a `DESTROY` boolean parameter switches from
dry-run to deletion.

```groovy
pipeline {
  agent any
  triggers { cron('H 3 * * *') }
  parameters { booleanParam(name: 'DESTROY', defaultValue: false, description: 'Really delete') }
  environment { NEXUS_BASE_URL = 'https://nexus.example.com' }
  stages {
    stage('Cleanup') {
      steps {
        withCredentials([usernamePassword(credentialsId: 'jenkins-ci',
            usernameVariable: 'NEXUS_USERNAME', passwordVariable: 'NEXUS_PASSWORD')]) {
          sh "nexus3-go cleanup --policy zextras-cleanup.yaml --max-requests 5000 ${params.DESTROY ? '--apply --compact' : ''}"
        }
      }
    }
  }
}
```

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
make build   # build ./bin/nexus3-go (version/commit/date injected via -ldflags)
make test    # go test -race -count=1 ./...
make lint    # golangci-lint run
make tidy    # go mod tidy (CI fails if go.mod/go.sum drift)
make vuln    # govulncheck ./...
```

`nexus3-go version` (or `--version`) prints the version, commit, and build
date; `go install ...@vX.Y.Z` builds report the module version instead.

The `Dockerfile` expects goreleaser's `dockers_v2` build context
(`<os>/<arch>/nexus3-go`), so build images with `goreleaser release
--snapshot --clean` rather than a plain `docker build`.

See `make help` for the full target list.

## License

[MIT](LICENSE)

## Disclaimer

This project is not affiliated with, endorsed by, or supported by Sonatype.
"Nexus" and "Sonatype" are trademarks of Sonatype, Inc.
