package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3"
)

const (
	kindDeb    = "deb"
	kindRpm    = "rpm"
	kindRaw    = "raw"
	kindMaven2 = "maven2"
)

// simpleUploadKinds are formats needing nothing beyond --base-url,
// --repository, and one or more --file/positional paths; they all dispatch
// through Client.UploadSimple.
var simpleUploadKinds = map[string]bool{
	"go": true, "helm": true, "npm": true, "nuget": true, "pypi": true, "rubygems": true,
}

// stringList implements flag.Value as a repeatable string flag: every
// occurrence of the flag on the command line appends to the slice instead of
// overwriting it, which is how --file and --asset accept multiple values.
type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// collectFiles expands --file values plus trailing positional arguments into
// a single ordered list of paths. A value containing a glob metacharacter
// (*, ?, or [) is expanded with filepath.Glob and must match at least one
// path; a value without one is kept exactly as given, so a typo'd literal
// path surfaces as a real "file not found" error from the upload call
// instead of silently vanishing from the batch. Order is preserved and
// duplicates are left alone (repeating a path just uploads it twice).
func collectFiles(kind string, fileFlags, positional []string) ([]string, error) {
	var raw []string
	raw = append(raw, fileFlags...)
	raw = append(raw, positional...)

	files := make([]string, 0, len(raw))
	for _, v := range raw {
		if !strings.ContainsAny(v, "*?[") {
			files = append(files, v)
			continue
		}
		matches, err := filepath.Glob(v)
		if err != nil {
			return nil, fmt.Errorf("upload %s: invalid glob pattern %q: %w", kind, v, err)
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("upload %s: glob pattern %q matched no files", kind, v)
		}
		files = append(files, matches...)
	}
	return files, nil
}

// parseMaven2Asset parses a --asset value of the form
// "path[:extension[:classifier]]". SplitN caps the split at 3 fields, so a
// path that itself contains ':' beyond the first two separators is kept
// intact in the classifier rather than being chopped further.
func parseMaven2Asset(v string) (nexus3.Maven2Asset, error) {
	fields := strings.SplitN(v, ":", 3)
	if fields[0] == "" {
		return nexus3.Maven2Asset{}, fmt.Errorf("upload maven2: --asset %q: path must not be empty", v)
	}

	asset := nexus3.Maven2Asset{Path: fields[0]}
	if len(fields) > 1 {
		asset.Extension = fields[1]
	}
	if len(fields) > 2 {
		asset.Classifier = fields[2]
	}
	return asset, nil
}

// runBatchUpload drives upload over files with up to concurrency workers via
// nexus3.UploadBatch, then reports the whole outcome at once: on full
// success it prints a one-line summary and returns nil, and on any failure
// it returns a single joined error whose first line states how many of the
// files made it and whose remaining lines each name one failing path, so a
// caller never has to guess which uploads in a large batch actually failed.
func runBatchUpload(
	ctx context.Context,
	kind, repository string,
	files []string,
	concurrency int,
	upload func(ctx context.Context, path string) error,
) error {
	results := nexus3.UploadBatch(ctx, concurrency, files, upload)

	failures := make([]error, 0, len(results))
	for _, r := range results {
		if r.Err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", r.Path, r.Err))
		}
	}
	if len(failures) > 0 {
		summary := fmt.Errorf("upload %s: uploaded %d/%d files", kind, len(files)-len(failures), len(files))
		return errors.Join(append([]error{summary}, failures...)...)
	}

	fmt.Printf("uploaded %d file(s) to %s\n", len(files), repository)
	return nil
}

// runUpload handles "nexus3-go upload <kind> ..." directly, bypassing
// Restish entirely. See pkg/nexus3.UploadDeb/UploadRpm for why: Nexus's own
// OpenAPI/swagger.json declares no request body for the upload endpoint, so
// Restish's spec-driven generated commands can never expose the required
// multipart fields — only this hand-wired path can perform the upload.
// Non-maven2 kinds fan their files out across nexus3.UploadBatch and report
// every failure, not just the first; maven2 assets share one set of
// coordinates and always travel in a single createComponents request, so
// they bypass the batch helper and go straight through UploadMaven2Assets.
func runUpload(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nexus3-go upload <kind> --base-url URL --repository NAME " +
			"(--file PATH [--file PATH ...] | PATH ...) [--concurrency N] [kind-specific flags]\n" +
			"kinds: deb, rpm, raw, maven2, go, helm, npm, nuget, pypi, rubygems\n" +
			"--file is repeatable and accepts glob patterns (*, ?, [...]); trailing positional " +
			"arguments are treated as files too\n" +
			"maven2 multi-asset components use repeatable --asset path[:extension[:classifier]] instead of --file")
	}

	kind := args[0]
	fs := flag.NewFlagSet("upload "+kind, flag.ContinueOnError)
	baseURL := fs.String("base-url", os.Getenv("NEXUS_BASE_URL"), "Nexus base URL (env NEXUS_BASE_URL)")
	repository := fs.String("repository", "", "destination repository name")
	var fileFlags stringList
	fs.Var(&fileFlags, "file", "path to upload, repeatable; accepts glob patterns (*, ?, [...]); "+
		"trailing positional arguments are treated as files too")
	var assetFlags stringList
	fs.Var(&assetFlags, "asset", "maven2 asset as path[:extension[:classifier]], repeatable (kind=maven2 only)")
	concurrency := fs.Int("concurrency", 4, "max parallel uploads in flight for non-maven2 kinds (must be >= 1)")
	directory := fs.String("directory", "", "raw repository directory (kind=raw only)")
	groupID := fs.String("group-id", "", "Maven group ID (kind=maven2 only)")
	artifactID := fs.String("artifact-id", "", "Maven artifact ID (kind=maven2 only)")
	version := fs.String("version", "", "Maven version (kind=maven2 only)")
	extension := fs.String("extension", "", "Maven asset extension, e.g. jar (kind=maven2 only, --file form)")
	classifier := fs.String("classifier", "", "Maven asset classifier (kind=maven2 only, --file form)")
	packaging := fs.String("packaging", "", "Maven packaging (kind=maven2 only)")
	generatePOM := fs.Bool("generate-pom", false, "generate a pom.xml for these coordinates (kind=maven2 only)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	if *baseURL == "" || *repository == "" {
		return fmt.Errorf("upload %s: --base-url and --repository are required", kind)
	}
	if *concurrency < 1 {
		return fmt.Errorf("upload %s: --concurrency must be >= 1, got %d", kind, *concurrency)
	}
	if kind != kindMaven2 && len(assetFlags) > 0 {
		return fmt.Errorf("upload %s: --asset is only valid for kind=maven2", kind)
	}

	files, err := collectFiles(kind, fileFlags, fs.Args())
	if err != nil {
		return err
	}
	if kind != kindMaven2 && len(files) == 0 {
		return fmt.Errorf("upload %s: at least one --file is required", kind)
	}

	username := os.Getenv("NEXUS_USERNAME")
	password := os.Getenv("NEXUS_PASSWORD")
	if username == "" || password == "" {
		return fmt.Errorf("upload %s: NEXUS_USERNAME and NEXUS_PASSWORD env vars are required", kind)
	}

	client := nexus3.New(*baseURL, nexus3.WithBasicAuth(username, password))
	ctx := context.Background()

	switch {
	case kind == kindMaven2:
		if *groupID == "" || *artifactID == "" || *version == "" {
			return fmt.Errorf("upload maven2: --group-id, --artifact-id, and --version are all required")
		}
		coords := nexus3.Maven2Coordinates{
			GroupID:     *groupID,
			ArtifactID:  *artifactID,
			Version:     *version,
			Packaging:   *packaging,
			GeneratePOM: *generatePOM,
		}

		var assets []nexus3.Maven2Asset
		switch {
		case len(assetFlags) > 0 && len(files) > 0:
			return fmt.Errorf("upload maven2: --asset and --file are mutually exclusive")
		case len(assetFlags) > 0:
			if *extension != "" || *classifier != "" {
				return fmt.Errorf("upload maven2: --extension and --classifier apply only to the single " +
					"--file form; put per-asset metadata in the --asset value instead")
			}
			assets = make([]nexus3.Maven2Asset, 0, len(assetFlags))
			for _, v := range assetFlags {
				asset, err := parseMaven2Asset(v)
				if err != nil {
					return err
				}
				assets = append(assets, asset)
			}
		case len(files) > 0:
			if len(files) > 1 {
				return fmt.Errorf("upload maven2: --file matched %d files, want exactly 1; "+
					"use --asset for a multi-asset maven2 component", len(files))
			}
			assets = []nexus3.Maven2Asset{{Path: files[0], Extension: *extension, Classifier: *classifier}}
		default:
			return fmt.Errorf("upload maven2: --file or --asset is required")
		}

		if len(assets) > nexus3.MaxMaven2Assets {
			return fmt.Errorf("upload maven2: %d assets exceeds the %d-asset limit Nexus's createComponents "+
				"API enforces per component", len(assets), nexus3.MaxMaven2Assets)
		}

		if err := client.UploadMaven2Assets(ctx, *repository, &coords, assets); err != nil {
			return err
		}

		fmt.Printf("uploaded %d asset(s) to %s\n", len(assets), *repository)

		return nil
	case kind == kindDeb:
		return runBatchUpload(ctx, kind, *repository, files, *concurrency, func(ctx context.Context, path string) error {
			return client.UploadDeb(ctx, *repository, path)
		})
	case kind == kindRpm:
		return runBatchUpload(ctx, kind, *repository, files, *concurrency, func(ctx context.Context, path string) error {
			return client.UploadRpm(ctx, *repository, path)
		})
	case kind == kindRaw:
		if *directory == "" {
			return fmt.Errorf("upload raw: --directory is required")
		}
		return runBatchUpload(ctx, kind, *repository, files, *concurrency, func(ctx context.Context, path string) error {
			return client.UploadRaw(ctx, *repository, *directory, path)
		})
	case simpleUploadKinds[kind]:
		return runBatchUpload(ctx, kind, *repository, files, *concurrency, func(ctx context.Context, path string) error {
			return client.UploadSimple(ctx, kind, *repository, path)
		})
	default:
		return fmt.Errorf("upload: unsupported kind %q, want one of deb, rpm, raw, maven2, go, helm, npm, nuget, pypi, rubygems", kind)
	}
}
