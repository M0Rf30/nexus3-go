package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunUpload_Help(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := runUploadContext(context.Background(), []string{arg}, &stdout, &stderr); err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
			for _, kind := range uploadKinds() {
				if !strings.Contains(stdout.String(), kind) {
					t.Errorf("usage missing kind %q:\n%s", kind, stdout.String())
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRunUpload_NoArgsUsageOnStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runUploadContext(context.Background(), nil, &stdout, &stderr); err == nil {
		t.Fatal("error = nil, want non-nil")
	}
	if !strings.Contains(stderr.String(), "usage:") || stdout.Len() != 0 {
		t.Errorf("stderr = %q, stdout = %q; want usage on stderr only", stderr.String(), stdout.String())
	}
}

func TestRunUpload_KindHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runUploadContext(context.Background(), []string{"deb", "-h"}, &stdout, &stderr); err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if !strings.Contains(stdout.String(), "-base-url") || !strings.Contains(stdout.String(), "-timeout") {
		t.Errorf("flag defaults missing from stdout:\n%s", stdout.String())
	}
}

func TestRun_Version(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"nexus3-go", arg}, &stdout, &stderr); code != 0 {
			t.Fatalf("run(%s) = %d, want 0", arg, code)
		}
		if got := stdout.String(); !strings.HasPrefix(got, "nexus3-go ") || !strings.Contains(got, buildVersion()) {
			t.Errorf("run(%s) output = %q, want it to contain %q", arg, got, buildVersion())
		}
	}
}

func TestBuildVersion_Injected(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "v9.9.9"
	if got := buildVersion(); got != "v9.9.9" {
		t.Errorf("buildVersion() = %q, want v9.9.9", got)
	}
}

func TestRun_UploadErrorExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"nexus3-go", "upload"}, &stdout, &stderr); code != 1 {
		t.Errorf("run(upload) = %d, want 1", code)
	}
	if code := run([]string{"nexus3-go", "upload", "-h"}, &stdout, &stderr); code != 0 {
		t.Errorf("run(upload -h) = %d, want 0", code)
	}
}

func TestRunUpload_TimeoutAndUserAgent(t *testing.T) {
	release := make(chan struct{})
	uaCh := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case uaCh <- r.Header.Get("User-Agent"):
		default:
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "slow.deb")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := runUploadContext(context.Background(), []string{
		"deb", "--base-url", srv.URL, "--repository", "r", "--file", path, "--timeout", "100ms",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("error = nil, want deadline error")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") || !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want deadline exceeded naming %s", err, path)
	}
	gotUA := <-uaCh
	if !strings.HasPrefix(gotUA, "nexus3-go/") {
		t.Errorf("User-Agent = %q, want nexus3-go/ prefix", gotUA)
	}
}

func TestRunUpload_CancelledContext(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "a.deb")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	var stdout, stderr bytes.Buffer
	err := runUploadContext(ctx, []string{"deb", "--base-url", "http://unused.invalid", "--repository", "r", "--file", path}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want cancellation error naming %s", err, path)
	}
}
