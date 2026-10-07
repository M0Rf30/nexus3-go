package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3"
)

const (
	kindDeb    = "deb"
	kindRpm    = "rpm"
	kindRaw    = "raw"
	kindMaven2 = "maven2"
)

// uploadKinds returns every kind "upload" accepts: the hand-wired deb, rpm,
// raw, and maven2 kinds followed by the formats nexus3.SimpleFormats lists
// (those all dispatch through Client.UploadSimple). It is the single source
// for usage text, error messages, and kind validation.
func uploadKinds() []string {
	return append([]string{kindDeb, kindRpm, kindRaw, kindMaven2}, nexus3.SimpleFormats()...)
}

// kindsList renders uploadKinds as a comma-separated string.
func kindsList() string {
	return strings.Join(uploadKinds(), ", ")
}

// isUploadKind reports whether kind is one of uploadKinds.
func isUploadKind(kind string) bool {
	return slices.Contains(uploadKinds(), kind)
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
// success it prints a one-line summary to stdout and returns nil, and on any
// failure it returns a single joined error whose first line states how many
// of the files made it and whose remaining lines each name one failing path,
// so a caller never has to guess which uploads in a large batch actually
// failed. Files skipped because ctx was cancelled are reported as failures
// carrying the context error.
func runBatchUpload(
	ctx context.Context,
	stdout io.Writer,
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

	_, _ = fmt.Fprintf(stdout, "uploaded %d file(s) to %s\n", len(files), repository)
	return nil
}

// uploadOptions holds every value parsed from the "upload <kind>" flags.
type uploadOptions struct {
	kind        string
	baseURL     string
	repository  string
	files       stringList
	assets      stringList
	concurrency int
	timeout     time.Duration
	directory   string
	groupID     string
	artifactID  string
	version     string
	extension   string
	classifier  string
	packaging   string
	generatePOM bool
}

// newUploadFlagSet builds the FlagSet for kind, bound to a fresh
// uploadOptions. Its output is discarded while parsing so that usage is only
// printed where the caller decides (stdout for -h, nowhere for errors, which
// are returned instead).
func newUploadFlagSet(kind string) (*flag.FlagSet, *uploadOptions) {
	o := &uploadOptions{kind: kind}
	fs := flag.NewFlagSet("upload "+kind, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.baseURL, "base-url", os.Getenv("NEXUS_BASE_URL"), "Nexus base URL (env NEXUS_BASE_URL)")
	fs.StringVar(&o.repository, "repository", "", "destination repository name")
	fs.Var(&o.files, "file", "path to upload, repeatable; accepts glob patterns (*, ?, [...]); "+
		"trailing positional arguments are treated as files too")
	fs.Var(&o.assets, "asset", "maven2 asset as path[:extension[:classifier]], repeatable (kind=maven2 only)")
	fs.IntVar(&o.concurrency, "concurrency", 4, "max parallel uploads in flight for non-maven2 kinds (must be >= 1)")
	fs.DurationVar(&o.timeout, "timeout", 0, "abort the whole upload after this duration, e.g. 90s or 5m (0 = no limit)")
	fs.StringVar(&o.directory, "directory", "", "raw repository directory (kind=raw only)")
	fs.StringVar(&o.groupID, "group-id", "", "Maven group ID (kind=maven2 only)")
	fs.StringVar(&o.artifactID, "artifact-id", "", "Maven artifact ID (kind=maven2 only)")
	fs.StringVar(&o.version, "version", "", "Maven version (kind=maven2 only)")
	fs.StringVar(&o.extension, "extension", "", "Maven asset extension, e.g. jar (kind=maven2 only, --file form)")
	fs.StringVar(&o.classifier, "classifier", "", "Maven asset classifier (kind=maven2 only, --file form)")
	fs.StringVar(&o.packaging, "packaging", "", "Maven packaging (kind=maven2 only)")
	fs.BoolVar(&o.generatePOM, "generate-pom", false, "generate a pom.xml for these coordinates (kind=maven2 only)")
	fs.Usage = func() {
		w := fs.Output()
		_, _ = fmt.Fprintf(w, "usage: nexus3-go upload %s --base-url URL --repository NAME "+
			"(--file PATH [--file PATH ...] | PATH ...) [flags]\n\nflags:\n", kind)
		fs.PrintDefaults()
	}

	return fs, o
}

// printUploadUsage writes the general "upload" usage, including the kinds
// list, to w.
func printUploadUsage(w io.Writer) {
	_, _ = fmt.Fprintf(w, "usage: nexus3-go upload <kind> --base-url URL --repository NAME "+
		"(--file PATH [--file PATH ...] | PATH ...) [--concurrency N] [--timeout DURATION] [kind-specific flags]\n"+
		"kinds: %s\n"+
		"--file is repeatable and accepts glob patterns (*, ?, [...]); trailing positional "+
		"arguments are treated as files too\n"+
		"maven2 multi-asset components use repeatable --asset path[:extension[:classifier]] instead of --file\n"+
		"run \"nexus3-go upload <kind> -h\" for the flags of one kind\n"+
		"environment: NEXUS_BASE_URL, NEXUS_USERNAME, NEXUS_PASSWORD\n", kindsList())
}

// runUpload handles "nexus3-go upload <kind> ..." with a background context
// and the process's standard streams. See runUploadContext.
func runUpload(args []string) error {
	return runUploadContext(context.Background(), args, os.Stdout, os.Stderr)
}

// runUploadContext handles "nexus3-go upload <kind> ..." directly, bypassing
// Restish entirely. See pkg/nexus3.UploadDeb/UploadRpm for why: Nexus's own
// OpenAPI/swagger.json declares no request body for the upload endpoint, so
// Restish's spec-driven generated commands can never expose the required
// multipart fields — only this hand-wired path can perform the upload.
// Non-maven2 kinds fan their files out across nexus3.UploadBatch and report
// every failure, not just the first; maven2 assets share one set of
// coordinates and always travel in a single createComponents request, so
// they bypass the batch helper and go straight through UploadMaven2Assets.
//
// Help requests (-h, --help, help as the kind, or -h after a kind) print
// usage to stdout and succeed; a missing kind prints usage to stderr and
// fails. Cancelling ctx aborts in-flight uploads.
func runUploadContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUploadUsage(stderr)
		return errors.New("upload: missing kind")
	}

	kind := args[0]
	switch kind {
	case "-h", "--help", "-help", "help":
		printUploadUsage(stdout)
		return nil
	}
	if !isUploadKind(kind) {
		return fmt.Errorf("upload: unsupported kind %q, want one of %s", kind, kindsList())
	}

	fs, opts := newUploadFlagSet(kind)
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fs.Usage()
			return nil
		}
		return fmt.Errorf("upload %s: %w", kind, err)
	}

	files, err := opts.validate(fs.Args())
	if err != nil {
		return err
	}

	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}

	client := nexus3.New(opts.baseURL,
		nexus3.WithBasicAuth(os.Getenv("NEXUS_USERNAME"), os.Getenv("NEXUS_PASSWORD")),
		nexus3.WithUserAgent("nexus3-go/"+buildVersion()))

	return opts.dispatch(ctx, stdout, client, files)
}

// validate checks the options common to every kind and returns the expanded
// file list.
func (o *uploadOptions) validate(positional []string) ([]string, error) {
	if o.baseURL == "" || o.repository == "" {
		return nil, fmt.Errorf("upload %s: --base-url and --repository are required", o.kind)
	}
	if o.concurrency < 1 {
		return nil, fmt.Errorf("upload %s: --concurrency must be >= 1, got %d", o.kind, o.concurrency)
	}
	if o.timeout < 0 {
		return nil, fmt.Errorf("upload %s: --timeout must be >= 0, got %s", o.kind, o.timeout)
	}
	if o.kind != kindMaven2 && len(o.assets) > 0 {
		return nil, fmt.Errorf("upload %s: --asset is only valid for kind=maven2", o.kind)
	}

	files, err := collectFiles(o.kind, o.files, positional)
	if err != nil {
		return nil, err
	}
	if o.kind != kindMaven2 && len(files) == 0 {
		return nil, fmt.Errorf("upload %s: at least one --file is required", o.kind)
	}

	if os.Getenv("NEXUS_USERNAME") == "" || os.Getenv("NEXUS_PASSWORD") == "" {
		return nil, fmt.Errorf("upload %s: NEXUS_USERNAME and NEXUS_PASSWORD env vars are required", o.kind)
	}

	return files, nil
}

// dispatch runs the upload for o.kind.
func (o *uploadOptions) dispatch(ctx context.Context, stdout io.Writer, client *nexus3.Client, files []string) error {
	batch := func(upload func(ctx context.Context, path string) error) error {
		return runBatchUpload(ctx, stdout, o.kind, o.repository, files, o.concurrency, upload)
	}

	switch o.kind {
	case kindMaven2:
		return o.uploadMaven2(ctx, stdout, client, files)
	case kindDeb:
		return batch(func(ctx context.Context, path string) error {
			return client.UploadDeb(ctx, o.repository, path)
		})
	case kindRpm:
		return batch(func(ctx context.Context, path string) error {
			return client.UploadRpm(ctx, o.repository, path)
		})
	case kindRaw:
		if o.directory == "" {
			return fmt.Errorf("upload raw: --directory is required")
		}
		return batch(func(ctx context.Context, path string) error {
			return client.UploadRaw(ctx, o.repository, o.directory, path)
		})
	default:
		return batch(func(ctx context.Context, path string) error {
			return client.UploadSimple(ctx, o.kind, o.repository, path)
		})
	}
}

// uploadMaven2 builds the asset list from --file or --asset and uploads them
// as a single component.
func (o *uploadOptions) uploadMaven2(ctx context.Context, stdout io.Writer, client *nexus3.Client, files []string) error {
	if o.groupID == "" || o.artifactID == "" || o.version == "" {
		return fmt.Errorf("upload maven2: --group-id, --artifact-id, and --version are all required")
	}
	coords := nexus3.Maven2Coordinates{
		GroupID:     o.groupID,
		ArtifactID:  o.artifactID,
		Version:     o.version,
		Packaging:   o.packaging,
		GeneratePOM: o.generatePOM,
	}

	var assets []nexus3.Maven2Asset
	switch {
	case len(o.assets) > 0 && len(files) > 0:
		return fmt.Errorf("upload maven2: --asset and --file are mutually exclusive")
	case len(o.assets) > 0:
		if o.extension != "" || o.classifier != "" {
			return fmt.Errorf("upload maven2: --extension and --classifier apply only to the single " +
				"--file form; put per-asset metadata in the --asset value instead")
		}
		assets = make([]nexus3.Maven2Asset, 0, len(o.assets))
		for _, v := range o.assets {
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
		assets = []nexus3.Maven2Asset{{Path: files[0], Extension: o.extension, Classifier: o.classifier}}
	default:
		return fmt.Errorf("upload maven2: --file or --asset is required")
	}

	if len(assets) > nexus3.MaxMaven2Assets {
		return fmt.Errorf("upload maven2: %d assets exceeds the %d-asset limit Nexus's createComponents "+
			"API enforces per component", len(assets), nexus3.MaxMaven2Assets)
	}

	if err := client.UploadMaven2Assets(ctx, o.repository, &coords, assets); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "uploaded %d asset(s) to %s\n", len(assets), o.repository)

	return nil
}
