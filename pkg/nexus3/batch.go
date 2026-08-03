package nexus3

import (
	"context"
	"sync"
)

// UploadResult reports the outcome of uploading one path within a
// UploadBatch call. Err is nil on success.
type UploadResult struct {
	Path string
	Err  error
}

// UploadBatch runs upload over paths with at most concurrency workers in
// flight, returning one result per path in the same order as paths.
// concurrency <= 0 means 1, and is never raised above len(paths).
//
// UploadBatch never aborts early because one path failed — every path gets
// a result. ctx cancellation instead stops scheduling new work: any path
// not yet started by the time its worker picks it up gets a result
// carrying ctx.Err() instead of running upload.
func UploadBatch(
	ctx context.Context,
	concurrency int,
	paths []string,
	upload func(ctx context.Context, path string) error,
) []UploadResult {
	results := make([]UploadResult, len(paths))
	if len(paths) == 0 {
		return results
	}

	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(paths) {
		concurrency = len(paths)
	}

	work := make(chan int)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			for i := range work {
				results[i].Path = paths[i]
				if err := ctx.Err(); err != nil {
					results[i].Err = err
					continue
				}
				results[i].Err = upload(ctx, paths[i])
			}
		}()
	}

	for i := range paths {
		work <- i
	}
	close(work)
	wg.Wait()

	return results
}
