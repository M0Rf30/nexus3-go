package nexus3

import (
	"context"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// ListRepositories returns every repository configured on the server.
//
// It wraps RepositoryManagementAPI.GetAllRepositories (GET /v1/repositories),
// the "RepositoryManagementAPI" field on *v3.APIClient.
func (c *Client) ListRepositories(ctx context.Context) ([]v3.RepositoryXO, error) {
	repos, resp, err := c.api.RepositoryManagementAPI.GetAllRepositories(c.authCtx(ctx)).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, wrapAPIError("list repositories", resp, err)
	}
	return repos, nil
}
