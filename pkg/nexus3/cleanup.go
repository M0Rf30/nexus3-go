package nexus3

import (
	"context"
	"errors"

	v3 "github.com/sonatype-nexus-community/nexus-repo-api-client-go/v3"
)

// TaskTypeCompactBlobStore is the Nexus task type of the "Admin - Compact
// blob store" task. Compacting is what actually reclaims disk space after
// components have been deleted.
const TaskTypeCompactBlobStore = "blobstore.compact"

// DeleteComponent deletes the component with the given id (as reported by
// ComponentXO.Id), together with all of its assets.
//
// It wraps ComponentsAPI.DeleteComponents (DELETE /v1/components/{id}). The
// blobs are only soft-deleted; run a blob store compact task to free space.
func (c *Client) DeleteComponent(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("nexus3: delete component: empty component id")
	}
	resp, err := c.api.ComponentsAPI.DeleteComponents(c.authCtx(ctx), id).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return wrapAPIError("delete component", resp, err)
	}
	return nil
}

// ListTasks returns the scheduled tasks configured on the server. When
// taskType is non-empty only tasks of that type (for example
// TaskTypeCompactBlobStore) are returned; "" returns every task.
//
// It wraps TasksAPI.ListTasks (GET /v1/tasks). The endpoint accepts no
// continuation token, so a single request returns the complete list.
func (c *Client) ListTasks(ctx context.Context, taskType string) ([]v3.TaskXO, error) {
	req := c.api.TasksAPI.ListTasks(c.authCtx(ctx))
	if taskType != "" {
		req = req.Type_(taskType)
	}
	page, resp, err := req.Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return nil, wrapAPIError("list tasks", resp, err)
	}
	return page.GetItems(), nil
}

// RunTask starts the task with the given id (as reported by TaskXO.Id) and
// returns once the server has accepted the request; it does not wait for the
// task to finish.
//
// It wraps TasksAPI.CreateTasksRun (POST /v1/tasks/{id}/run).
func (c *Client) RunTask(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("nexus3: run task: empty task id")
	}
	resp, err := c.api.TasksAPI.CreateTasksRun(c.authCtx(ctx), id).Execute()
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		return wrapAPIError("run task", resp, err)
	}
	return nil
}
