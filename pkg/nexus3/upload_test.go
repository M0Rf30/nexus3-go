package nexus3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestUploadDeb(t *testing.T) {
	var gotRepo, gotFilename, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/rest/v1/components" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		gotRepo = r.URL.Query().Get("repository")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		file, header, err := r.FormFile("apt.asset")
		if err != nil {
			t.Fatalf("read apt.asset part: %v", err)
		}
		defer func() { _ = file.Close() }()
		gotFilename = header.Filename
		body, _ := io.ReadAll(file)
		gotBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	path := writeTempFile(t, "example_amd64.deb", "deb-content")
	client := New(srv.URL)
	if err := client.UploadDeb(context.Background(), "apt-hosted", path); err != nil {
		t.Fatalf("UploadDeb() error = %v", err)
	}
	if gotRepo != "apt-hosted" {
		t.Errorf("repository = %q, want %q", gotRepo, "apt-hosted")
	}
	if gotFilename != "example_amd64.deb" {
		t.Errorf("filename = %q, want %q", gotFilename, "example_amd64.deb")
	}
	if gotBody != "deb-content" {
		t.Errorf("body = %q, want %q", gotBody, "deb-content")
	}
}

func TestUploadRpm(t *testing.T) {
	var gotRepo, gotAssetFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRepo = r.URL.Query().Get("repository")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotAssetFilename = r.FormValue("yum.asset.filename")
		if _, _, err := r.FormFile("yum.asset"); err != nil {
			t.Fatalf("read yum.asset part: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	path := writeTempFile(t, "example.el9.x86_64.rpm", "rpm-content")
	client := New(srv.URL)
	if err := client.UploadRpm(context.Background(), "yum-hosted", path); err != nil {
		t.Fatalf("UploadRpm() error = %v", err)
	}
	if gotRepo != "yum-hosted" {
		t.Errorf("repository = %q, want %q", gotRepo, "yum-hosted")
	}
	if gotAssetFilename != "example.el9.x86_64.rpm" {
		t.Errorf("yum.asset.filename = %q, want %q", gotAssetFilename, "example.el9.x86_64.rpm")
	}
}

func TestUploadDeb_MissingFile(t *testing.T) {
	client := New("http://unused.invalid")
	err := client.UploadDeb(context.Background(), "apt-hosted", "/nonexistent/path.deb")
	if err == nil || !strings.Contains(err.Error(), "upload deb") {
		t.Errorf("UploadDeb() error = %v, want an 'upload deb' wrapped error", err)
	}
}

func TestUploadDeb_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	path := writeTempFile(t, "example.deb", "x")
	client := New(srv.URL)
	if err := client.UploadDeb(context.Background(), "apt-hosted", path); err == nil {
		t.Error("UploadDeb() error = nil, want error for 403 response")
	}
}

func TestUploadRaw(t *testing.T) {
	var gotRepo, gotDirectory, gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRepo = r.URL.Query().Get("repository")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotDirectory = r.FormValue("raw.directory")
		_, header, err := r.FormFile("raw.asset1")
		if err != nil {
			t.Fatalf("read raw.asset1 part: %v", err)
		}
		gotFilename = header.Filename
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	path := writeTempFile(t, "notes.txt", "raw-content")
	client := New(srv.URL)
	if err := client.UploadRaw(context.Background(), "raw-hosted", "docs/notes", path); err != nil {
		t.Fatalf("UploadRaw() error = %v", err)
	}
	if gotRepo != "raw-hosted" {
		t.Errorf("repository = %q, want %q", gotRepo, "raw-hosted")
	}
	if gotDirectory != "docs/notes" {
		t.Errorf("raw.directory = %q, want %q", gotDirectory, "docs/notes")
	}
	if gotFilename != "notes.txt" {
		t.Errorf("filename = %q, want %q", gotFilename, "notes.txt")
	}
}

func TestUploadMaven2(t *testing.T) {
	var gotRepo, gotGroup, gotArtifact, gotVersion, gotExt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRepo = r.URL.Query().Get("repository")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		gotGroup = r.FormValue("maven2.groupId")
		gotArtifact = r.FormValue("maven2.artifactId")
		gotVersion = r.FormValue("maven2.version")
		gotExt = r.FormValue("maven2.asset1.extension")
		if _, _, err := r.FormFile("maven2.asset1"); err != nil {
			t.Fatalf("read maven2.asset1 part: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	path := writeTempFile(t, "lib.jar", "jar-content")
	client := New(srv.URL)
	coords := Maven2Coordinates{GroupID: "com.example", ArtifactID: "lib", Version: "1.0", Extension: "jar"}
	if err := client.UploadMaven2(context.Background(), "maven-hosted", &coords, path); err != nil {
		t.Fatalf("UploadMaven2() error = %v", err)
	}
	if gotRepo != "maven-hosted" {
		t.Errorf("repository = %q, want %q", gotRepo, "maven-hosted")
	}
	if gotGroup != "com.example" || gotArtifact != "lib" || gotVersion != "1.0" {
		t.Errorf("coordinates = %s:%s:%s, want com.example:lib:1.0", gotGroup, gotArtifact, gotVersion)
	}
	if gotExt != "jar" {
		t.Errorf("maven2.asset1.extension = %q, want %q", gotExt, "jar")
	}
}

func TestUploadSimple(t *testing.T) {
	var gotRepo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRepo = r.URL.Query().Get("repository")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if _, _, err := r.FormFile("npm.asset"); err != nil {
			t.Fatalf("read npm.asset part: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	path := writeTempFile(t, "pkg.tgz", "tarball-content")
	client := New(srv.URL)
	if err := client.UploadSimple(context.Background(), "npm", "npm-hosted", path); err != nil {
		t.Fatalf("UploadSimple() error = %v", err)
	}
	if gotRepo != "npm-hosted" {
		t.Errorf("repository = %q, want %q", gotRepo, "npm-hosted")
	}
}

func TestUploadSimple_UnsupportedFormat(t *testing.T) {
	client := New("http://unused.invalid")
	err := client.UploadSimple(context.Background(), "docker", "any-repo", "/nonexistent")
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("UploadSimple() error = %v, want unsupported-format error", err)
	}
}
