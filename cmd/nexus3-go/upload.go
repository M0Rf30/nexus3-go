package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3"
)

const (
	kindDeb    = "deb"
	kindRpm    = "rpm"
	kindRaw    = "raw"
	kindMaven2 = "maven2"
)

// simpleUploadKinds are formats needing nothing beyond --base-url,
// --repository, and --file; they all dispatch through Client.UploadSimple.
var simpleUploadKinds = map[string]bool{
	"go": true, "helm": true, "npm": true, "nuget": true, "pypi": true, "rubygems": true,
}

// runUpload handles "nexus3-go upload <kind> ..." directly, bypassing
// Restish entirely. See pkg/nexus3.UploadDeb/UploadRpm for why: Nexus's own
// OpenAPI/swagger.json declares no request body for the upload endpoint, so
// Restish's spec-driven generated commands can never expose the required
// multipart fields — only this hand-wired path can perform the upload.
func runUpload(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nexus3-go upload <kind> --base-url URL --repository NAME --file PATH [kind-specific flags]\n" +
			"kinds: deb, rpm, raw, maven2, go, helm, npm, nuget, pypi, rubygems")
	}

	kind := args[0]
	fs := flag.NewFlagSet("upload "+kind, flag.ContinueOnError)
	baseURL := fs.String("base-url", os.Getenv("NEXUS_BASE_URL"), "Nexus base URL (env NEXUS_BASE_URL)")
	repository := fs.String("repository", "", "destination repository name")
	filePath := fs.String("file", "", "path to the file to upload")
	directory := fs.String("directory", "", "raw repository directory (kind=raw only)")
	groupID := fs.String("group-id", "", "Maven group ID (kind=maven2 only)")
	artifactID := fs.String("artifact-id", "", "Maven artifact ID (kind=maven2 only)")
	version := fs.String("version", "", "Maven version (kind=maven2 only)")
	extension := fs.String("extension", "", "Maven asset extension, e.g. jar (kind=maven2 only)")
	classifier := fs.String("classifier", "", "Maven asset classifier (kind=maven2 only)")
	packaging := fs.String("packaging", "", "Maven packaging (kind=maven2 only)")
	generatePOM := fs.Bool("generate-pom", false, "generate a pom.xml for these coordinates (kind=maven2 only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *baseURL == "" || *repository == "" || *filePath == "" {
		return fmt.Errorf("upload %s: --base-url, --repository, and --file are all required", kind)
	}

	username := os.Getenv("NEXUS_USERNAME")
	password := os.Getenv("NEXUS_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("upload %s: NEXUS_USERNAME and NEXUS_PASSWORD env vars are required", kind)
	}

	client := nexus3.New(*baseURL, nexus3.WithBasicAuth(username, password))
	ctx := context.Background()

	switch {
	case kind == kindDeb:
		return client.UploadDeb(ctx, *repository, *filePath)
	case kind == kindRpm:
		return client.UploadRpm(ctx, *repository, *filePath)
	case kind == kindRaw:
		if *directory == "" {
			return fmt.Errorf("upload raw: --directory is required")
		}
		return client.UploadRaw(ctx, *repository, *directory, *filePath)
	case kind == kindMaven2:
		if *groupID == "" || *artifactID == "" || *version == "" {
			return fmt.Errorf("upload maven2: --group-id, --artifact-id, and --version are all required")
		}
		coords := nexus3.Maven2Coordinates{
			GroupID:     *groupID,
			ArtifactID:  *artifactID,
			Version:     *version,
			Extension:   *extension,
			Classifier:  *classifier,
			Packaging:   *packaging,
			GeneratePOM: *generatePOM,
		}
		return client.UploadMaven2(ctx, *repository, &coords, *filePath)
	case simpleUploadKinds[kind]:
		return client.UploadSimple(ctx, kind, *repository, *filePath)
	default:
		return fmt.Errorf("upload: unsupported kind %q, want one of deb, rpm, raw, maven2, go, helm, npm, nuget, pypi, rubygems", kind)
	}
}
