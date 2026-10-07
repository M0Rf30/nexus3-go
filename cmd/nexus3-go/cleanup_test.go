package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/M0Rf30/nexus3-go/pkg/nexus3/cleanup"
)

const testPolicy = `policies:
  - name: keep2
    repositories: [apt-hosted]
    keepLatest: 2
`

// fakeNexus is an httptest Nexus serving 5 apt components (c1..c5, c5 newest)
// over two pages, plus a configurable task list.
type fakeNexus struct {
	srv     *httptest.Server
	mu      sync.Mutex
	deletes []string
	runs    []string
	tasks   string
	failID  string
}

func newFakeNexus(t *testing.T, tasksJSON string) *fakeNexus {
	t.Helper()

	f := &fakeNexus{tasks: tasksJSON}
	mux := http.NewServeMux()
	comp := func(i int) string {
		return fmt.Sprintf(`{"id":"c%d","repository":"apt-hosted","format":"apt","group":"amd64","name":"foo",`+
			`"version":"1.0-%d","assets":[{"path":"foo_%d.deb","fileSize":1024,`+
			`"blobCreated":"2026-06-03T00:00:00.000+00:00"}]}`, i, i, i)
	}

	mux.HandleFunc("GET /service/rest/v1/components", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Query().Get("continuationToken") == "" {
			_, _ = fmt.Fprintf(w, `{"items":[%s,%s,%s],"continuationToken":"next"}`, comp(1), comp(2), comp(3))
			return
		}

		_, _ = fmt.Fprintf(w, `{"items":[%s,%s],"continuationToken":null}`, comp(4), comp(5))
	})
	mux.HandleFunc("DELETE /service/rest/v1/components/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		f.mu.Lock()
		f.deletes = append(f.deletes, id)
		f.mu.Unlock()

		if id == f.failID {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /service/rest/v1/tasks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, f.tasks)
	})
	mux.HandleFunc("POST /service/rest/v1/tasks/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.runs = append(f.runs, r.PathValue("id"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)

	return f
}

func writePolicy(t *testing.T) string {
	t.Helper()

	p := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(p, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func runCleanup(t *testing.T, f *fakeNexus, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	t.Setenv("NEXUS_BASE_URL", "")

	args := append([]string{"--policy", writePolicy(t), "--base-url", f.srv.URL}, extra...)

	var out, errb bytes.Buffer
	err = runCleanupContext(context.Background(), args, &out, &errb)

	return out.String(), errb.String(), err
}

func TestCleanup_DryRun(t *testing.T) {
	f := newFakeNexus(t, `{"items":[]}`)

	out, _, err := runCleanup(t, f)
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(f.deletes) != 0 {
		t.Errorf("dry-run deletes = %v, want none", f.deletes)
	}

	for _, id := range []string{"1.0-1", "1.0-2", "1.0-3"} {
		if !strings.Contains(out, id) {
			t.Errorf("plan output missing %s:\n%s", id, out)
		}
	}

	if strings.Contains(out, "1.0-5") || !strings.Contains(out, "3 items") {
		t.Errorf("unexpected plan output:\n%s", out)
	}
}

func TestCleanup_Apply(t *testing.T) {
	f := newFakeNexus(t, `{"items":[]}`)

	if _, _, err := runCleanup(t, f, "--apply"); err != nil {
		t.Fatalf("err = %v", err)
	}

	got := map[string]bool{}
	for _, id := range f.deletes {
		got[id] = true
	}

	if len(f.deletes) != 3 || !got["c1"] || !got["c2"] || !got["c3"] {
		t.Errorf("deletes = %v, want c1,c2,c3", f.deletes)
	}
}

func TestCleanup_ApplyFailure(t *testing.T) {
	f := newFakeNexus(t, `{"items":[]}`)
	f.failID = "c2"

	out, _, err := runCleanup(t, f, "--apply")
	if err == nil {
		t.Fatal("err = nil, want delete failure")
	}

	if !strings.Contains(out, "FAILED") {
		t.Errorf("output lacks FAILED:\n%s", out)
	}
}

func TestCleanup_JSON(t *testing.T) {
	f := newFakeNexus(t, `{"items":[]}`)

	out, _, err := runCleanup(t, f, "--output", "json", "--apply")
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	var rep struct {
		Plan    cleanup.Plan `json:"plan"`
		Applied bool         `json:"applied"`
		Results []struct {
			Error string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}

	if len(rep.Plan.Items) != 3 || !rep.Applied || len(rep.Results) != 3 {
		t.Errorf("report = %+v", rep)
	}
}

func TestCleanup_Compact(t *testing.T) {
	f := newFakeNexus(t, `{"items":[{"id":"t1","name":"compact","type":"blobstore.compact"},`+
		`{"id":"t2","name":"compact2","type":"blobstore.compact"}]}`)

	if _, _, err := runCleanup(t, f, "--apply", "--compact"); err != nil {
		t.Fatalf("err = %v", err)
	}

	if len(f.runs) != 2 {
		t.Errorf("runs = %v, want t1,t2", f.runs)
	}
}

func TestCleanup_CompactNoTasksWarns(t *testing.T) {
	f := newFakeNexus(t, `{"items":[]}`)

	_, stderr, err := runCleanup(t, f, "--apply", "--compact")
	if err != nil {
		t.Fatalf("err = %v", err)
	}

	if !strings.Contains(stderr, "warning") {
		t.Errorf("stderr = %q, want warning", stderr)
	}
}

func TestCleanup_CompactRequiresApply(t *testing.T) {
	f := newFakeNexus(t, `{"items":[]}`)

	if _, _, err := runCleanup(t, f, "--compact"); err == nil {
		t.Fatal("err = nil, want --compact requires --apply")
	}
}

func TestCleanup_UsageErrors(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	t.Setenv("NEXUS_BASE_URL", "")

	tests := []struct {
		name string
		args []string
	}{
		{"missing policy", []string{"--base-url", "http://x.invalid"}},
		{"missing base url", []string{"--policy", "p.yaml"}},
		{"bad output", []string{"--policy", "p.yaml", "--base-url", "http://x.invalid", "--output", "xml"}},
		{"bad concurrency", []string{"--policy", "p.yaml", "--base-url", "http://x.invalid", "--concurrency", "0"}},
		{"missing file", []string{"--policy", "/nonexistent.yaml", "--base-url", "http://x.invalid"}},
		{"unknown flag", []string{"--bogus"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if err := runCleanupContext(context.Background(), tt.args, &out, &errb); err == nil {
				t.Error("err = nil, want error")
			}
		})
	}
}

func TestCleanup_Help(t *testing.T) {
	var out, errb bytes.Buffer
	if err := runCleanupContext(context.Background(), []string{"-h"}, &out, &errb); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}

	if !strings.Contains(out.String(), "--policy") {
		t.Errorf("usage missing: %q", out.String())
	}
}

func TestCleanup_ExamplePolicyParses(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "zextras-cleanup.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := cleanup.ParseConfig(data)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}

	if len(cfg.Policies) != 4 {
		t.Errorf("policies = %d, want 4", len(cfg.Policies))
	}
}

func TestHumanBytes(t *testing.T) {
	tests := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 << 20: "5.0 MiB"}
	for in, want := range tests {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
