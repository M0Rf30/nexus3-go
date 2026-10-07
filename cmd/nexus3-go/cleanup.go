package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3"
	"github.com/M0Rf30/nexus3-go/pkg/nexus3/cleanup"
	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// cleanupOptions holds every value parsed from the "cleanup" flags.
type cleanupOptions struct {
	policy      string
	baseURL     string
	apply       bool
	concurrency int
	maxRequests int
	output      string
	compact     bool
	timeout     time.Duration
}

// cleanupClient is the subset of *nexus3.Client the cleanup command uses.
type cleanupClient interface {
	cleanup.API
	ListTasks(ctx context.Context, taskType string) ([]v3.TaskXO, error)
	RunTask(ctx context.Context, id string) error
}

// newCleanupFlagSet builds the FlagSet for "cleanup", bound to a fresh
// cleanupOptions. Output is discarded while parsing; usage is printed only
// where the caller decides.
func newCleanupFlagSet() (*flag.FlagSet, *cleanupOptions) {
	o := &cleanupOptions{}
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.policy, "policy", "", "cleanup policy YAML file (required)")
	fs.StringVar(&o.baseURL, "base-url", os.Getenv("NEXUS_BASE_URL"), "Nexus base URL (env NEXUS_BASE_URL)")
	fs.BoolVar(&o.apply, "apply", false, "actually delete the planned components (default: dry-run)")
	fs.IntVar(&o.concurrency, "concurrency", 4, "max parallel deletes in flight (must be >= 1)")
	fs.IntVar(&o.maxRequests, "max-requests", 0, "request budget for listing plus deleting (0 = unlimited)")
	fs.StringVar(&o.output, "output", "text", "output format: text or json")
	fs.BoolVar(&o.compact, "compact", false, "after deleting, run every blobstore compact task (requires --apply)")
	fs.DurationVar(&o.timeout, "timeout", 0, "abort after this duration, e.g. 10m (0 = no limit)")
	fs.Usage = func() {
		w := fs.Output()
		_, _ = fmt.Fprint(w, "usage: nexus3-go cleanup --policy FILE [--base-url URL] [--apply] "+
			"[--concurrency N] [--max-requests N] [--output text|json] [--compact] [--timeout DURATION]\n\n"+
			"Dry-run unless --apply is given. Environment: NEXUS_BASE_URL, NEXUS_USERNAME, NEXUS_PASSWORD\n\nflags:\n")
		fs.PrintDefaults()
	}

	return fs, o
}

// runCleanupContext handles "nexus3-go cleanup ..." directly, bypassing
// Restish. -h/--help print usage to stdout and succeed. A non-nil error means
// exit code 1 (bad usage, planning failure, or any failed delete).
func runCleanupContext(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs, opts := newCleanupFlagSet()
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fs.SetOutput(stdout)
			fs.Usage()

			return nil
		}

		fs.SetOutput(stderr)

		return fmt.Errorf("cleanup: %w", err)
	}

	if err := opts.validate(fs.NArg()); err != nil {
		return err
	}

	cfg, err := cleanup.LoadConfig(opts.policy)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}

	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}

	client := nexus3.New(opts.baseURL,
		nexus3.WithBasicAuth(os.Getenv("NEXUS_USERNAME"), os.Getenv("NEXUS_PASSWORD")),
		nexus3.WithUserAgent("nexus3-go/"+buildVersion()))

	return opts.execute(ctx, stdout, stderr, client, cfg)
}

// validate checks the parsed options.
func (o *cleanupOptions) validate(positional int) error {
	switch {
	case positional > 0:
		return errors.New("cleanup: unexpected positional arguments")
	case o.policy == "":
		return errors.New("cleanup: --policy is required")
	case o.baseURL == "":
		return errors.New("cleanup: --base-url (or NEXUS_BASE_URL) is required")
	case o.concurrency < 1:
		return fmt.Errorf("cleanup: --concurrency must be >= 1, got %d", o.concurrency)
	case o.maxRequests < 0:
		return fmt.Errorf("cleanup: --max-requests must be >= 0, got %d", o.maxRequests)
	case o.timeout < 0:
		return fmt.Errorf("cleanup: --timeout must be >= 0, got %s", o.timeout)
	case o.output != "text" && o.output != "json":
		return fmt.Errorf("cleanup: --output must be text or json, got %q", o.output)
	case o.compact && !o.apply:
		return errors.New("cleanup: --compact requires --apply")
	}

	if os.Getenv("NEXUS_USERNAME") == "" || os.Getenv("NEXUS_PASSWORD") == "" {
		return errors.New("cleanup: NEXUS_USERNAME and NEXUS_PASSWORD env vars are required")
	}

	return nil
}

// jsonResult is the JSON form of a cleanup.Result.
type jsonResult struct {
	Item  cleanup.Item `json:"item"`
	Error string       `json:"error,omitempty"`
}

// jsonReport is the document printed by --output json.
type jsonReport struct {
	Plan    *cleanup.Plan `json:"plan"`
	Applied bool          `json:"applied"`
	Results []jsonResult  `json:"results,omitempty"`
}

// execute plans, optionally applies and compacts, and prints the outcome.
func (o *cleanupOptions) execute(
	ctx context.Context, stdout, stderr io.Writer, client cleanupClient, cfg *cleanup.Config,
) error {
	engine := cleanup.NewEngine(client, cleanup.Options{MaxRequests: o.maxRequests})

	plan, err := engine.Plan(ctx, cfg)
	if err != nil {
		return fmt.Errorf("cleanup: plan: %w", err)
	}

	var results []cleanup.Result
	if o.apply {
		results = engine.Apply(ctx, plan, o.concurrency)
	}

	failed := 0

	for i := range results {
		if results[i].Err != nil {
			failed++
		}
	}

	if err := o.report(stdout, plan, results); err != nil {
		return err
	}

	var compactErr error
	if o.compact {
		compactErr = runCompact(ctx, stdout, stderr, client)
	}

	var errs []error
	if failed > 0 {
		errs = append(errs, fmt.Errorf("cleanup: %d of %d deletes failed", failed, len(results)))
	}

	if compactErr != nil {
		errs = append(errs, compactErr)
	}

	return errors.Join(errs...)
}

// report prints the plan (and results when applied) in the chosen format.
func (o *cleanupOptions) report(w io.Writer, plan *cleanup.Plan, results []cleanup.Result) error {
	if o.output == "json" {
		rep := jsonReport{Plan: plan, Applied: o.apply}
		for i := range results {
			jr := jsonResult{Item: results[i].Item}
			if results[i].Err != nil {
				jr.Error = results[i].Err.Error()
			}

			rep.Results = append(rep.Results, jr)
		}

		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")

		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("cleanup: encode json: %w", err)
		}

		return nil
	}

	errByID := make(map[string]error, len(results))
	for i := range results {
		if results[i].Err != nil {
			errByID[results[i].Item.ID] = results[i].Err
		}
	}

	verb := "would delete"
	if o.apply {
		verb = "deleted"
	}

	for i := range plan.Items {
		it := &plan.Items[i]
		v := verb
		suffix := ""

		if err, bad := errByID[it.ID]; bad {
			v = "FAILED"
			suffix = ": " + err.Error()
		}

		_, _ = fmt.Fprintf(w, "%s %s %s/%s %s (%s) [%s]%s\n", v, it.Repository, it.Group, it.Name,
			it.Version, humanBytes(it.Size), it.Reason, suffix)
	}

	mode := "dry-run"
	if o.apply {
		mode = "applied"
	}

	_, _ = fmt.Fprintf(w, "%s: %d items, %s to free, scanned %d, kept %d, ~%d requests\n",
		mode, len(plan.Items), humanBytes(plan.Bytes), plan.Scanned, plan.Kept, plan.EstimatedRequests)

	return nil
}

// runCompact runs every blobstore compact task, warning when none exist.
func runCompact(ctx context.Context, stdout, stderr io.Writer, client cleanupClient) error {
	tasks, err := client.ListTasks(ctx, nexus3.TaskTypeCompactBlobStore)
	if err != nil {
		return fmt.Errorf("cleanup: compact: %w", err)
	}

	if len(tasks) == 0 {
		_, _ = fmt.Fprintf(stderr, "warning: no %s tasks exist; create one in Nexus to reclaim disk space\n",
			nexus3.TaskTypeCompactBlobStore)

		return nil
	}

	var errs []error

	for i := range tasks {
		t := &tasks[i]
		id := t.GetId()
		if err := client.RunTask(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("cleanup: compact: run task %s: %w", id, err))
			continue
		}

		_, _ = fmt.Fprintf(stdout, "started compact task %s (%s)\n", t.GetName(), id)
	}

	return errors.Join(errs...)
}

// humanBytes renders n with a binary unit suffix.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
