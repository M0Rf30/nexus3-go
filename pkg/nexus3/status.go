package nexus3

import (
	"context"
)

// Status performs the basic Nexus health check (GET /v1/status) and returns
// nil when the server responds with a 2xx status, or a wrapped error
// otherwise.
func (c *Client) Status(ctx context.Context) error {
	resp, err := c.api.StatusAPI.ListStatus(c.authCtx(ctx)).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return wrapAPIError("status", resp, err)
	}
	return nil
}
