package nexus3

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// SearchComponents searches for components in repository matching query and
// returns the first page of results plus a continuation token. To fetch
// subsequent pages, pass the token to SearchComponentsPage together with the
// same repository and query (do NOT pass it to ListComponents: search and
// list tokens are not interchangeable). AllSearchResults follows the tokens
// automatically. An empty repository or query matches everything.
//
// It wraps SearchAPI.ListSearch (GET /v1/search).
func (c *Client) SearchComponents(ctx context.Context, repository, query string) ([]v3.ComponentXO, string, error) {
	return c.SearchComponentsPage(ctx, repository, query, "")
}

// SearchComponentsPage is like SearchComponents but resumes from
// continuationToken, a token previously returned by SearchComponents or
// SearchComponentsPage for the same repository and query. Pass "" for the
// first page. An empty returned token means there are no more pages.
//
// It wraps SearchAPI.ListSearch (GET /v1/search).
func (c *Client) SearchComponentsPage(ctx context.Context, repository, query, continuationToken string) ([]v3.ComponentXO, string, error) {
	req := c.api.SearchAPI.ListSearch(c.authCtx(ctx))
	if repository != "" {
		req = req.Repository(repository)
	}
	if query != "" {
		req = req.Q(query)
	}
	if continuationToken != "" {
		req = req.ContinuationToken(continuationToken)
	}

	page, resp, err := req.Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, "", wrapAPIError("search components", resp, err)
	}
	return page.GetItems(), page.GetContinuationToken(), nil
}

// ListComponents returns the page of components in repository following
// continuationToken (pass "" to fetch the first page), plus the token for
// the next page. The token must come from a previous ListComponents call;
// search tokens belong to SearchComponentsPage.
//
// It wraps ComponentsAPI.ListComponents (GET /v1/components). The generated
// client does not decode this endpoint's JSON body itself, so the response
// is parsed here.
func (c *Client) ListComponents(ctx context.Context, repository, continuationToken string) ([]v3.ComponentXO, string, error) {
	req := c.api.ComponentsAPI.ListComponents(c.authCtx(ctx))
	if repository != "" {
		req = req.Repository(repository)
	}
	if continuationToken != "" {
		req = req.ContinuationToken(continuationToken)
	}

	resp, err := req.Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, "", wrapAPIError("list components", resp, err)
	}

	var page v3.PageComponentXO
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("nexus3: list components: decode response: %w", err)
	}
	return page.GetItems(), page.GetContinuationToken(), nil
}

// AllComponents returns an iterator over every component in repository,
// transparently following continuation tokens page by page. Iteration stops
// when the consumer breaks, when ctx is cancelled between pages (yielding
// ctx.Err()), or after yielding a single non-nil error on failure.
func (c *Client) AllComponents(ctx context.Context, repository string) iter.Seq2[v3.ComponentXO, error] {
	return paginate(ctx, func(ctx context.Context, token string) ([]v3.ComponentXO, string, error) {
		return c.ListComponents(ctx, repository, token)
	})
}

// AllSearchResults returns an iterator over every component matching query
// in repository, transparently following search continuation tokens. It
// stops on the same conditions as AllComponents.
func (c *Client) AllSearchResults(ctx context.Context, repository, query string) iter.Seq2[v3.ComponentXO, error] {
	return paginate(ctx, func(ctx context.Context, token string) ([]v3.ComponentXO, string, error) {
		return c.SearchComponentsPage(ctx, repository, query, token)
	})
}

// paginate drives fetch until it returns an empty continuation token.
func paginate(ctx context.Context, fetch func(ctx context.Context, token string) ([]v3.ComponentXO, string, error)) iter.Seq2[v3.ComponentXO, error] {
	return func(yield func(v3.ComponentXO, error) bool) {
		token := ""
		for {
			if err := ctx.Err(); err != nil {
				yield(v3.ComponentXO{}, err)
				return
			}
			items, next, err := fetch(ctx, token)
			if err != nil {
				yield(v3.ComponentXO{}, err)
				return
			}
			for _, item := range items {
				if !yield(item, nil) {
					return
				}
			}
			if next == "" {
				return
			}
			token = next
		}
	}
}
