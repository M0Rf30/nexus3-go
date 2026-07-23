package nexus3

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_Defaults(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(srv.URL)
	if err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if want := "/service/rest/v1/status"; gotPath != want {
		t.Errorf("request path = %q, want %q", gotPath, want)
	}
}

func TestWithBasicAuth_SetsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(srv.URL, WithBasicAuth("admin", "admin123"))
	if err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("Authorization header = %q, want prefix %q", gotAuth, "Basic ")
	}
}

func TestStatus(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "healthy", statusCode: http.StatusOK, wantErr: false},
		{name: "unhealthy", statusCode: http.StatusServiceUnavailable, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer srv.Close()

			client := New(srv.URL)
			err := client.Status(context.Background())
			if (err != nil) != tt.wantErr {
				t.Errorf("Status() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestListRepositories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/rest/v1/repositories" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"name": "maven-central", "format": "maven2", "type": "proxy", "url": "http://example/repo"},
		})
	}))
	defer srv.Close()

	client := New(srv.URL)
	repos, err := client.ListRepositories(context.Background())
	if err != nil {
		t.Fatalf("ListRepositories() error = %v", err)
	}
	if len(repos) != 1 || repos[0].GetName() != "maven-central" {
		t.Errorf("ListRepositories() = %+v, want one repo named maven-central", repos)
	}
}

func TestSearchComponents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/rest/v1/search" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items":             []map[string]any{{"name": "my-app", "format": "maven2"}},
			"continuationToken": "next-page-token",
		})
	}))
	defer srv.Close()

	client := New(srv.URL)
	items, token, err := client.SearchComponents(context.Background(), "maven-releases", "my-app")
	if err != nil {
		t.Fatalf("SearchComponents() error = %v", err)
	}
	if len(items) != 1 || items[0].GetName() != "my-app" {
		t.Errorf("SearchComponents() items = %+v, want one component named my-app", items)
	}
	if token != "next-page-token" {
		t.Errorf("SearchComponents() token = %q, want %q", token, "next-page-token")
	}
}
