package nexus3

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeleteComponent(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL, WithBasicAuth("u", "p"))
	if err := c.DeleteComponent(context.Background(), "abc123"); err != nil {
		t.Fatalf("DeleteComponent: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/service/rest/v1/components/abc123" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("Authorization = %q", gotAuth)
	}
}

func TestRunTask(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := New(srv.URL).RunTask(context.Background(), "t-1"); err != nil {
		t.Fatalf("RunTask: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/service/rest/v1/tasks/t-1/run" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

func TestListTasks(t *testing.T) {
	tests := []struct {
		name      string
		taskType  string
		wantQuery string
		wantHasQ  bool
	}{
		{name: "all tasks", taskType: "", wantHasQ: false},
		{name: "filtered by type", taskType: TaskTypeCompactBlobStore, wantQuery: "blobstore.compact", wantHasQ: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotQuery string
			var hasQ bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/service/rest/v1/tasks" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				gotQuery = r.URL.Query().Get("type")
				hasQ = r.URL.Query().Has("type")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"items":[{"id":"t1","name":"compact","type":"blobstore.compact"},{"id":"t2","name":"other","type":"x"}],"continuationToken":null}`))
			}))
			defer srv.Close()

			tasks, err := New(srv.URL).ListTasks(context.Background(), tt.taskType)
			if err != nil {
				t.Fatalf("ListTasks: %v", err)
			}
			if gotQuery != tt.wantQuery || hasQ != tt.wantHasQ {
				t.Errorf("type query = %q (present=%v), want %q (present=%v)", gotQuery, hasQ, tt.wantQuery, tt.wantHasQ)
			}
			if len(tasks) != 2 || tasks[0].GetId() != "t1" || tasks[0].GetType() != "blobstore.compact" || tasks[1].GetName() != "other" {
				t.Errorf("tasks = %+v", tasks)
			}
		})
	}
}

func TestCleanupMethods_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found here", http.StatusNotFound)
	}))
	defer srv.Close()
	c := New(srv.URL)
	ctx := context.Background()

	tests := []struct {
		name   string
		prefix string
		call   func() error
	}{
		{"delete component", "nexus3: delete component: ", func() error { return c.DeleteComponent(ctx, "nope") }},
		{"run task", "nexus3: run task: ", func() error { return c.RunTask(ctx, "nope") }},
		{"list tasks", "nexus3: list tasks: ", func() error { _, err := c.ListTasks(ctx, ""); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("errors.As(*APIError) failed for %v", err)
			}
			if apiErr.StatusCode != http.StatusNotFound || apiErr.Body != "not found here" {
				t.Errorf("got %+v", apiErr)
			}
			if !strings.HasPrefix(err.Error(), tt.prefix) {
				t.Errorf("error = %q, want prefix %q", err, tt.prefix)
			}
		})
	}
}

func TestCleanupMethods_EmptyID(t *testing.T) {
	c := New("http://127.0.0.1:1")
	ctx := context.Background()
	if err := c.DeleteComponent(ctx, ""); err == nil || !strings.HasPrefix(err.Error(), "nexus3: delete component: ") {
		t.Errorf("DeleteComponent(\"\") = %v", err)
	}
	if err := c.RunTask(ctx, ""); err == nil || !strings.HasPrefix(err.Error(), "nexus3: run task: ") {
		t.Errorf("RunTask(\"\") = %v", err)
	}
}

func TestCleanupMethods_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused
	c := New(srv.URL)
	err := c.DeleteComponent(context.Background(), "x")
	if err == nil || !strings.HasPrefix(err.Error(), "nexus3: delete component: ") {
		t.Errorf("err = %v", err)
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("network error must not be an APIError: %v", err)
	}
}
