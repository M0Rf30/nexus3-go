package nexus3

import (
	"context"
	"fmt"
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
		return fmt.Errorf("nexus3: status: %w", err)
	}
	return nil
}
