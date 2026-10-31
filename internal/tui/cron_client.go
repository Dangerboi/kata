package tui

import (
	"context"
	"encoding/json/v2"
	"strings"

	"go.kenn.io/kata/internal/cron"
	"go.kenn.io/kata/pkg/client/generated"
)

type cronAPI interface {
	ListCronJobs(context.Context, int64) ([]generated.CronJob, error)
	ListCronWorkflows(context.Context, int64) ([]generated.CronWorkflow, error)
	ListCronRuns(context.Context, int64) ([]generated.CronRun, error)
	ListCronProgress(ctx context.Context, projectID int64, parentUID string) (map[string]cron.RecurringProgress, error)
}

// ListCronJobs reads the selected project's current job definitions.
func (c *Client) ListCronJobs(ctx context.Context, projectID int64) ([]generated.CronJob, error) {
	client, err := c.generatedClient()
	if err != nil {
		return nil, err
	}
	wire, callErr := client.ListCronJobsWithResponse(ctx, &generated.ListCronJobsRequestOptions{
		PathParams: &generated.ListCronJobsPath{ProjectID: projectID},
	})
	if wire == nil {
		return nil, callErr
	}
	if err := decodeGeneratedResponse(wire.HTTPResponse, wire.Body, callErr, nil); err != nil {
		return nil, err
	}
	return wire.JSON200.Jobs, nil
}

// ListCronWorkflows reads the selected project's current workflow definitions.
func (c *Client) ListCronWorkflows(ctx context.Context, projectID int64) ([]generated.CronWorkflow, error) {
	client, err := c.generatedClient()
	if err != nil {
		return nil, err
	}
	wire, callErr := client.ListCronWorkflowsWithResponse(ctx, &generated.ListCronWorkflowsRequestOptions{
		PathParams: &generated.ListCronWorkflowsPath{ProjectID: projectID},
	})
	if wire == nil {
		return nil, callErr
	}
	if err := decodeGeneratedResponse(wire.HTTPResponse, wire.Body, callErr, nil); err != nil {
		return nil, err
	}
	return wire.JSON200.Workflows, nil
}

// ListCronRuns reads the newest 100 observations.
func (c *Client) ListCronRuns(ctx context.Context, projectID int64) ([]generated.CronRun, error) {
	client, err := c.generatedClient()
	if err != nil {
		return nil, err
	}
	query := &generated.ListCronRunsQuery{Limit: new(int64(100))}
	wire, callErr := client.ListCronRunsWithResponse(ctx, &generated.ListCronRunsRequestOptions{
		PathParams: &generated.ListCronRunsPath{ProjectID: projectID},
		Query:      query,
	})
	if wire == nil {
		return nil, callErr
	}
	if err := decodeGeneratedResponse(wire.HTTPResponse, wire.Body, callErr, nil); err != nil {
		return nil, err
	}
	return wire.JSON200.Runs, nil
}

// ListCronProgress reads the delivery checkpoints that native recurring jobs
// keep on their parent issue, keyed by job UID.
func (c *Client) ListCronProgress(ctx context.Context, projectID int64, parentUID string) (map[string]cron.RecurringProgress, error) {
	client, err := c.generatedClient()
	if err != nil {
		return nil, err
	}
	wire, callErr := client.GetIssueMetadataWithResponse(ctx, &generated.GetIssueMetadataRequestOptions{
		PathParams: &generated.GetIssueMetadataPath{ProjectID: projectID, Ref: parentUID},
	})
	if wire == nil {
		return nil, callErr
	}
	if err := decodeGeneratedResponse(wire.HTTPResponse, wire.Body, callErr, nil); err != nil {
		return nil, err
	}
	out := map[string]cron.RecurringProgress{}
	for key, value := range wire.JSON200.Issue.Metadata {
		jobUID, ok := strings.CutPrefix(key, "cron.progress.")
		if !ok {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var progress cron.RecurringProgress
		if err := json.Unmarshal(raw, &progress); err != nil {
			return nil, err
		}
		out[jobUID] = progress
	}
	return out, nil
}
