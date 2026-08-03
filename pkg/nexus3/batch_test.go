package nexus3

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestUploadBatch_OrderPreserved(t *testing.T) {
	paths := []string{"a", "b", "c", "d", "e"}
	// Stagger completion so results would come back out of order if
	// UploadBatch didn't pin each result to its input index.
	delays := map[string]time.Duration{
		"a": 20 * time.Millisecond,
		"b": 1 * time.Millisecond,
		"c": 15 * time.Millisecond,
		"d": 5 * time.Millisecond,
		"e": 10 * time.Millisecond,
	}
	upload := func(_ context.Context, path string) error {
		time.Sleep(delays[path])
		return nil
	}

	results := UploadBatch(context.Background(), 4, paths, upload)
	if len(results) != len(paths) {
		t.Fatalf("len(results) = %d, want %d", len(results), len(paths))
	}
	for i, p := range paths {
		if results[i].Path != p {
			t.Errorf("results[%d].Path = %q, want %q", i, results[i].Path, p)
		}
		if results[i].Err != nil {
			t.Errorf("results[%d].Err = %v, want nil", i, results[i].Err)
		}
	}
}

func TestUploadBatch_PartialFailure(t *testing.T) {
	paths := []string{"ok1", "bad1", "ok2", "bad2"}
	failing := map[string]bool{"bad1": true, "bad2": true}
	upload := func(_ context.Context, path string) error {
		if failing[path] {
			return fmt.Errorf("boom: %s", path)
		}
		return nil
	}

	results := UploadBatch(context.Background(), 2, paths, upload)
	for i, p := range paths {
		if failing[p] && results[i].Err == nil {
			t.Errorf("results[%d] (%s) Err = nil, want error", i, p)
		}
		if !failing[p] && results[i].Err != nil {
			t.Errorf("results[%d] (%s) Err = %v, want nil", i, p, results[i].Err)
		}
	}
}

func TestUploadBatch_NonPositiveConcurrencyProcessesAll(t *testing.T) {
	paths := []string{"1", "2", "3", "4", "5", "6"}
	var processed atomic.Int64
	upload := func(_ context.Context, _ string) error {
		processed.Add(1)
		return nil
	}

	for _, c := range []int{0, -1, -100} {
		processed.Store(0)
		results := UploadBatch(context.Background(), c, paths, upload)
		if len(results) != len(paths) {
			t.Errorf("concurrency %d: len(results) = %d, want %d", c, len(results), len(paths))
		}
		if got := processed.Load(); got != int64(len(paths)) {
			t.Errorf("concurrency %d: processed = %d, want %d", c, got, len(paths))
		}
		for i, r := range results {
			if r.Err != nil {
				t.Errorf("concurrency %d: results[%d].Err = %v, want nil", c, i, r.Err)
			}
		}
	}
}

func TestUploadBatch_ConcurrencyBounded(t *testing.T) {
	const limit = 3
	paths := make([]string, 20)
	for i := range paths {
		paths[i] = fmt.Sprintf("path-%d", i)
	}

	var inFlight, peak atomic.Int64
	upload := func(_ context.Context, _ string) error {
		cur := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		return nil
	}

	UploadBatch(context.Background(), limit, paths, upload)
	if got := peak.Load(); got > int64(limit) {
		t.Errorf("peak in-flight = %d, want <= %d", got, limit)
	}
	if got := peak.Load(); got < 2 {
		t.Errorf("peak in-flight = %d, want workers to actually overlap (test too weak otherwise)", got)
	}
}

func TestUploadBatch_PreCancelledContext(t *testing.T) {
	paths := []string{"a", "b", "c"}
	var calls atomic.Int64
	upload := func(_ context.Context, _ string) error {
		calls.Add(1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := UploadBatch(ctx, 2, paths, upload)
	if got := calls.Load(); got != 0 {
		t.Errorf("upload was called %d times, want 0 for a pre-cancelled context", got)
	}
	for i, r := range results {
		if r.Err == nil {
			t.Errorf("results[%d].Err = nil, want ctx.Err()", i)
		}
		if r.Path != paths[i] {
			t.Errorf("results[%d].Path = %q, want %q", i, r.Path, paths[i])
		}
	}
}

func TestUploadBatch_Empty(t *testing.T) {
	upload := func(_ context.Context, _ string) error {
		t.Fatal("upload should not be called for an empty path list")
		return nil
	}
	results := UploadBatch(context.Background(), 4, nil, upload)
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}
