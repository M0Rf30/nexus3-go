package nexus3

import (
	"context"
	"encoding/json"
	"fmt"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// SearchComponents searches for components in repository matching query and
// returns the first page of results plus a continuation token. Pass the
// token to ListComponents to fetch subsequent pages. An empty repository or
// query matches everything.
//
// It wraps SearchAPI.ListSearch (GET /v1/search).
func (c *Client) SearchComponents(ctx context.Context, repository, query string) ([]v3.ComponentXO, string, error) {
	req := c.api.SearchAPI.ListSearch(c.authCtx(ctx))
	if repository != "" {
		req = req.Repository(repository)
	}
	if query != "" {
		req = req.Q(query)
	}

	page, resp, err := req.Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, "", fmt.Errorf("nexus3: search components: %w", err)
	}
	return page.GetItems(), page.GetContinuationToken(), nil
}

// ListComponents returns the page of components in repository following
// continuationToken (pass "" to fetch the first page), plus the token for
// the next page.
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
	if err != nil {
		return nil, "", fmt.Errorf("nexus3: list components: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var page v3.PageComponentXO
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, "", fmt.Errorf("nexus3: list components: decode response: %w", err)
	}
	return page.GetItems(), page.GetContinuationToken(), nil
}
