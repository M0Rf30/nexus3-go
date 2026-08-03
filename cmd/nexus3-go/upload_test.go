package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestRunUpload_NoFilesRequiresFile(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")

	err := runUpload([]string{"deb", "--base-url", "http://unused.invalid", "--repository", "r"})
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Errorf("runUpload() error = %v, want error naming --file", err)
	}
}

func TestRunUpload_ConcurrencyMustBePositive(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "pkg.deb")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{
		"deb", "--base-url", "http://unused.invalid", "--repository", "r",
		"--file", path, "--concurrency", "0",
	})
	if err == nil || !strings.Contains(err.Error(), "--concurrency") {
		t.Errorf("runUpload() error = %v, want --concurrency error", err)
	}
}

func TestRunUpload_AssetOnlyForMaven2(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "pkg.deb")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{"deb", "--base-url", "http://unused.invalid", "--repository", "r", "--asset", path})
	if err == nil || !strings.Contains(err.Error(), "--asset") {
		t.Errorf("runUpload() error = %v, want error naming --asset", err)
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

func TestRunUpload_RepeatableFile(t *testing.T) {
	var mu sync.Mutex
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	dir := t.TempDir()

	args := []string{"deb", "--base-url", srv.URL, "--repository", "apt-hosted"}
	for i := range 3 {
		path := filepath.Join(dir, fmt.Sprintf("pkg%d.deb", i))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		args = append(args, "--file", path)
	}

	if err := runUpload(args); err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if count != 3 {
		t.Errorf("request count = %d, want 3", count)
	}
}

func TestRunUpload_GlobExpansion(t *testing.T) {
	var mu sync.Mutex
	var count int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	dir := t.TempDir()
	for _, name := range []string{"a.deb", "b.deb"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
	}

	err := runUpload([]string{
		"deb", "--base-url", srv.URL, "--repository", "apt-hosted",
		"--file", filepath.Join(dir, "*.deb"),
	})
	if err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if count != 2 {
		t.Errorf("request count = %d, want 2", count)
	}
}

func TestRunUpload_GlobNoMatch(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	pattern := filepath.Join(t.TempDir(), "*.xyz")

	err := runUpload([]string{"deb", "--base-url", "http://unused.invalid", "--repository", "r", "--file", pattern})
	if err == nil || !strings.Contains(err.Error(), pattern) {
		t.Errorf("runUpload() error = %v, want it to name pattern %s", err, pattern)
	}
}

func TestRunUpload_LiteralMissingFile(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "does-not-exist.deb")

	err := runUpload([]string{"deb", "--base-url", "http://unused.invalid", "--repository", "r", "--file", path})
	if err == nil {
		t.Fatal("runUpload() error = nil, want open error for missing file")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("runUpload() error = %v, want it to name path %s", err, path)
	}
}

func TestRunUpload_PartialFailure(t *testing.T) {
	dir := t.TempDir()
	goodPath := filepath.Join(dir, "good.deb")
	badPath := filepath.Join(dir, "bad.deb")
	for _, p := range []string{goodPath, badPath} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		headers := r.MultipartForm.File["apt.asset"]
		if len(headers) == 1 && headers[0].Filename == filepath.Base(badPath) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")

	err := runUpload([]string{
		"deb", "--base-url", srv.URL, "--repository", "apt-hosted",
		"--file", goodPath, "--file", badPath,
	})
	if err == nil {
		t.Fatal("runUpload() error = nil, want partial-failure error")
	}
	if !strings.Contains(err.Error(), "uploaded 1/2") {
		t.Errorf("runUpload() error = %v, want it to mention uploaded 1/2", err)
	}
	if !strings.Contains(err.Error(), badPath) {
		t.Errorf("runUpload() error = %v, want it to name failing path %s", err, badPath)
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

func TestRunUpload_Maven2MultiAsset(t *testing.T) {
	var requests int
	var gotExt1, gotClass2 string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if _, ok := r.MultipartForm.File["maven2.asset1"]; !ok {
			t.Error("missing maven2.asset1 file part")
		}
		if _, ok := r.MultipartForm.File["maven2.asset2"]; !ok {
			t.Error("missing maven2.asset2 file part")
		}
		gotExt1 = r.FormValue("maven2.asset1.extension")
		gotClass2 = r.FormValue("maven2.asset2.classifier")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	dir := t.TempDir()
	jarPath := filepath.Join(dir, "lib.jar")
	pomPath := filepath.Join(dir, "lib.pom")
	for _, p := range []string{jarPath, pomPath} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
	}

	err := runUpload([]string{
		"maven2", "--base-url", srv.URL, "--repository", "maven-hosted",
		"--group-id", "com.example", "--artifact-id", "lib", "--version", "1.0",
		"--asset", jarPath + ":jar",
		"--asset", pomPath + ":pom:sources",
	})
	if err != nil {
		t.Fatalf("runUpload() error = %v", err)
	}
	if requests != 1 {
		t.Errorf("request count = %d, want 1", requests)
	}
	if gotExt1 != "jar" {
		t.Errorf("maven2.asset1.extension = %q, want %q", gotExt1, "jar")
	}
	if gotClass2 != "sources" {
		t.Errorf("maven2.asset2.classifier = %q, want %q", gotClass2, "sources")
	}
}

func TestRunUpload_Maven2AssetLimit(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	dir := t.TempDir()

	args := []string{
		"maven2", "--base-url", "http://unused.invalid", "--repository", "r",
		"--group-id", "com.example", "--artifact-id", "lib", "--version", "1.0",
	}
	for i := range 4 {
		path := filepath.Join(dir, fmt.Sprintf("a%d.jar", i))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		args = append(args, "--asset", path)
	}

	err := runUpload(args)
	if err == nil || !strings.Contains(err.Error(), "3-asset limit") {
		t.Errorf("runUpload() error = %v, want error naming the 3-asset limit", err)
	}
}

func TestRunUpload_Maven2AssetAndFileMutuallyExclusive(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "lib.jar")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{
		"maven2", "--base-url", "http://unused.invalid", "--repository", "r",
		"--group-id", "com.example", "--artifact-id", "lib", "--version", "1.0",
		"--file", path, "--asset", path,
	})
	if err == nil || !strings.Contains(err.Error(), "--asset") || !strings.Contains(err.Error(), "--file") {
		t.Errorf("runUpload() error = %v, want mutually-exclusive error naming --asset and --file", err)
	}
}

func TestRunUpload_Maven2AssetAndExtensionMutuallyExclusive(t *testing.T) {
	t.Setenv("NEXUS_USERNAME", "admin")
	t.Setenv("NEXUS_PASSWORD", "secret")
	path := filepath.Join(t.TempDir(), "lib.jar")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	err := runUpload([]string{
		"maven2", "--base-url", "http://unused.invalid", "--repository", "r",
		"--group-id", "com.example", "--artifact-id", "lib", "--version", "1.0",
		"--asset", path, "--extension", "jar",
	})
	if err == nil || !strings.Contains(err.Error(), "--extension") {
		t.Errorf("runUpload() error = %v, want error naming --extension", err)
	}
}

func TestParseMaven2Asset(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		wantPath      string
		wantExtension string
		wantClassifer string
		wantErr       bool
	}{
		{name: "path only", in: "a.jar", wantPath: "a.jar"},
		{name: "extension", in: "a.pom:pom", wantPath: "a.pom", wantExtension: "pom"},
		{name: "extension and classifier", in: "a.jar:jar:sources", wantPath: "a.jar", wantExtension: "jar", wantClassifer: "sources"},
		{name: "empty path errors", in: ":jar", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, err := parseMaven2Asset(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseMaven2Asset(%q) error = nil, want error", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMaven2Asset(%q) error = %v", tt.in, err)
			}
			if asset.Path != tt.wantPath || asset.Extension != tt.wantExtension || asset.Classifier != tt.wantClassifer {
				t.Errorf("parseMaven2Asset(%q) = %+v, want {Path:%q Extension:%q Classifier:%q}",
					tt.in, asset, tt.wantPath, tt.wantExtension, tt.wantClassifer)
			}
		})
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
