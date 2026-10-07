package nexus3

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

func TestAPIError_Upload400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "  Repository does not allow updating assets\n", http.StatusBadRequest)
	}))
	defer srv.Close()

	path := writeTempFile(t, "a.deb", "x")
	err := New(srv.URL).UploadDeb(context.Background(), "apt", path)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*APIError) failed for %v", err)
	}
	if apiErr.StatusCode != 400 || apiErr.Body != "Repository does not allow updating assets" {
		t.Errorf("got %+v", apiErr)
	}
	if !strings.HasPrefix(err.Error(), "nexus3: upload deb ") || !strings.Contains(err.Error(), "does not allow updating") {
		t.Errorf("error = %q", err)
	}
	var generic *v3.GenericOpenAPIError
	if !errors.As(err, &generic) {
		t.Errorf("original GenericOpenAPIError not reachable via Unwrap")
	}
}

func TestAPIError_ListComponents404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such repository", http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := New(srv.URL).ListComponents(context.Background(), "nope", "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As(*APIError) failed for %v", err)
	}
	if apiErr.StatusCode != 404 || apiErr.Body != "no such repository" {
		t.Errorf("got %+v", apiErr)
	}
	if !strings.HasPrefix(err.Error(), "nexus3: list components: ") {
		t.Errorf("error = %q", err)
	}
}

func TestAPIError_BodyTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(strings.Repeat("a", 5000)))
	}))
	defer srv.Close()

	err := New(srv.URL).Status(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As failed for %v", err)
	}
	if len(apiErr.Body) > maxErrorBody+len("…") {
		t.Errorf("body length %d not capped", len(apiErr.Body))
	}
}

func TestSearchComponentsPage_SendsToken(t *testing.T) {
	var got, gotQ, gotRepo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("continuationToken")
		gotQ = r.URL.Query().Get("q")
		gotRepo = r.URL.Query().Get("repository")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"continuationToken":null}`))
	}))
	defer srv.Close()

	if _, _, err := New(srv.URL).SearchComponentsPage(context.Background(), "r", "foo*", "tok1"); err != nil {
		t.Fatal(err)
	}
	if got != "tok1" || gotQ != "foo*" || gotRepo != "r" {
		t.Errorf("token=%q q=%q repo=%q", got, gotQ, gotRepo)
	}
}

// pagedServer serves pages keyed by continuationToken: "" -> a,b (tok1);
// tok1 -> c (tok2); tok2 -> empty, no token. failAt, if set, returns 500 for that token.
func pagedServer(reqs *atomic.Int32, failAt string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs.Add(1)
		tok := r.URL.Query().Get("continuationToken")
		if failAt != "" && tok == failAt {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch tok {
		case "":
			_, _ = w.Write([]byte(`{"items":[{"id":"a"},{"id":"b"}],"continuationToken":"tok1"}`))
		case "tok1":
			_, _ = w.Write([]byte(`{"items":[{"id":"c"}],"continuationToken":"tok2"}`))
		default:
			_, _ = w.Write([]byte(`{"items":[],"continuationToken":null}`))
		}
	}))
}

func TestIterators_WalkPages(t *testing.T) {
	var reqs atomic.Int32
	srv := pagedServer(&reqs, "")
	defer srv.Close()
	c := New(srv.URL)

	for name, seq := range map[string]func() []string{
		"components": func() []string {
			var ids []string
			for item, err := range c.AllComponents(context.Background(), "r") {
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, item.GetId())
			}
			return ids
		},
		"search": func() []string {
			var ids []string
			for item, err := range c.AllSearchResults(context.Background(), "r", "q") {
				if err != nil {
					t.Fatal(err)
				}
				ids = append(ids, item.GetId())
			}
			return ids
		},
	} {
		reqs.Store(0)
		if got := fmt.Sprint(seq()); got != "[a b c]" {
			t.Errorf("%s: ids = %s", name, got)
		}
		if reqs.Load() != 3 {
			t.Errorf("%s: requests = %d, want 3", name, reqs.Load())
		}
	}
}

func TestIterators_EarlyBreak(t *testing.T) {
	var reqs atomic.Int32
	srv := pagedServer(&reqs, "")
	defer srv.Close()

	for item, err := range New(srv.URL).AllComponents(context.Background(), "r") {
		if err != nil {
			t.Fatal(err)
		}
		_ = item
		break
	}
	if reqs.Load() != 1 {
		t.Errorf("requests = %d, want 1", reqs.Load())
	}
}

func TestIterators_ErrorMidway(t *testing.T) {
	var reqs atomic.Int32
	srv := pagedServer(&reqs, "tok1")
	defer srv.Close()

	var ids []string
	var errs []error
	for item, err := range New(srv.URL).AllSearchResults(context.Background(), "r", "q") {
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ids = append(ids, item.GetId())
	}
	if fmt.Sprint(ids) != "[a b]" || len(errs) != 1 {
		t.Fatalf("ids=%v errs=%v", ids, errs)
	}
	var apiErr *APIError
	if !errors.As(errs[0], &apiErr) || apiErr.StatusCode != 500 {
		t.Errorf("err = %v", errs[0])
	}
}

func TestIterators_ContextCancelledBetweenPages(t *testing.T) {
	var reqs atomic.Int32
	srv := pagedServer(&reqs, "")
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var gotErr error
	n := 0
	for _, err := range New(srv.URL).AllComponents(ctx, "r") {
		if err != nil {
			gotErr = err
			continue
		}
		n++
		cancel()
	}
	if !errors.Is(gotErr, context.Canceled) || n != 2 || reqs.Load() != 1 {
		t.Errorf("err=%v n=%d reqs=%d", gotErr, n, reqs.Load())
	}
}
