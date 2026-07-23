package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUpload_MissingArgs(t *testing.T) {
	if err := runUpload(nil); err == nil {
		t.Error("runUpload(nil) error = nil, want usage error")
	}
}

func TestRunUpload_UnsupportedKind(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "pkg.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"deb.rpm", "--base-url", "http://unused.invalid", "--repository", "r", "--file", path})
	if err == nil {
		t.Fatal("runUpload() error = nil, want unsupported-kind error")
	}
	if !strings.Contains(err.Error(), "unsupported kind") {
		t.Errorf("runUpload() error = %v, want unsupported kind message", err)
	}
}

func TestRunUpload_MissingRequiredFlags(t *testing.T) {
	if err := runUpload([]string{"deb"}); err == nil {
		t.Error("runUpload() error = nil, want missing required flags error")
	}
}

func TestRunUpload_MissingCredentials(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "")
	t.Setenv("NEXUS_PASSWORD", "")
	err := runUpload([]string{"deb", "--base-url", "http://unused.invalid", "--repository", "r", "--file", "/nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "NEXUS_USERNAME") {
		t.Errorf("runUpload() error = %v, want missing-credentials error", err)
	}
}

func TestRunUpload_Deb(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")

	path := filepath.Join(t.TempDir(), "pkg_amd64.deb")
	if err := os.WriteFile(path, []byte("deb-content"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"deb", "--base-url", srv.URL, "--repository", "apt-hosted", "--file", path})
	if err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("Authorization header = %q, want Basic auth", gotAuth)
	}
}

func TestRunUpload_RawRequiresDirectory(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"raw", "--base-url", "http://unused.invalid", "--repository", "r", "--file", path})
	if err == nil || !strings.Contains(err.Error(), "--directory is required") {
		t.Errorf("runUpload() error = %v, want --directory required error", err)
	}
}

func TestRunUpload_Raw(t *testing.T) {
	var gotDirectory string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotDirectory = r.FormValue("raw.directory")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"raw", "--base-url", srv.URL, "--repository", "raw-hosted", "--file", path, "--directory", "docs"})
	if err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if gotDirectory != "docs" {
		t.Errorf("raw.directory = %q, want %q", gotDirectory, "docs")
	}
}

func TestRunUpload_Maven2RequiresCoordinates(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "lib.jar")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"maven2", "--base-url", "http://unused.invalid", "--repository", "r", "--file", path})
	if err == nil || !strings.Contains(err.Error(), "--group-id") {
		t.Errorf("runUpload() error = %v, want missing-coordinates error", err)
	}
}

func TestRunUpload_Maven2(t *testing.T) {
	var gotGroup string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotGroup = r.FormValue("maven2.groupId")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "lib.jar")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{
		"maven2", "--base-url", srv.URL, "--repository", "maven-hosted", "--file", path,
		"--group-id", "com.example", "--artifact-id", "lib", "--version", "1.0",
	})
	if err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if gotGroup != "com.example" {
		t.Errorf("maven2.groupId = %q, want %q", gotGroup, "com.example")
	}
}

func TestRunUpload_SimpleFormat(t *testing.T) {
	var gotRepo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRepo = r.URL.Query().Get("repository")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "pkg.tgz")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"npm", "--base-url", srv.URL, "--repository", "npm-hosted", "--file", path})
	if err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if gotRepo != "npm-hosted" {
		t.Errorf("repository = %q, want %q", gotRepo, "npm-hosted")
	}
}
