package tui

import (
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
	"go.kenn.io/kata/internal/cron"
	"go.kenn.io/kata/internal/testenv"
	"go.kenn.io/kata/pkg/client/generated"
)

const (
	nextJobUID    = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	nextParentUID = "01ARZ3NDEKTSV4RRFFQ69G5FB9"
)

// nextRunsDetail opens the fixture job after giving it a trigger and, for a
// native job, the progress its parent issue would hold.
func nextRunsDetail(t *testing.T, now string, edit func(*generated.CronJobDefinitionResponse), progress *cron.RecurringProgress) string {
	t.Helper()
	at, err := time.Parse(time.RFC3339, now)
	require.NoError(t, err)
	prior := renderNow
	renderNow = func() time.Time { return at }
	t.Cleanup(func() { renderNow = prior })
	m := cronFixture(t)
	def := m.cron.data.jobs[0].Definition
	edit(&def)
	m.cron.data.jobs[0].Definition = def
	if progress != nil {
		m.cron.data.progress = map[string]cron.RecurringProgress{nextJobUID: *progress}
	}
	m = sendKey(m, tea.KeyEnter)
	return ansi.Strip(m.viewBody())
}

func nativeNotify(trigger generated.TriggerResponse) func(*generated.CronJobDefinitionResponse) {
	return func(d *generated.CronJobDefinitionResponse) {
		d.Trigger = trigger
		d.Action = generated.ActionResponse{Kind: "notify", Recipient: new("current-owner-or-author")}
		d.Issue = &generated.IssuePolicyResponse{Kind: "per-run", ParentUID: new(nextParentUID), Title: new("Review")}
		d.Overlap, d.Catchup = "allow", "all"
	}
}

func TestCronJobDetailNativeNextRunsMatchDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, cron, zone, now string
		want                  []string
	}{
		{"spring forward", "30 2 * * *", "America/New_York", "2026-03-07T08:00:00Z",
			[]string{"2026-03-08 03:30 EDT", "2026-03-09 02:30 EDT", "2026-03-10 02:30 EDT"}},
		{"fall back", "30 1 * * *", "America/New_York", "2026-11-01T04:00:00Z",
			[]string{"2026-11-01 01:30 EDT", "2026-11-01 01:30 EST", "2026-11-02 01:30 EST"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			require.NoError(t, err)
			progress := cron.RecurringProgress{Version: 1, Anchor: now, Through: now}
			got := nextRunsDetail(t, tc.now, nativeNotify(generated.TriggerResponse{Kind: "cron", Cron: new(tc.cron), Timezone: new(tc.zone)}), &progress)
			require.Regexp(t, `Next:\s+`+tc.want[0], got)
			for _, line := range tc.want {
				require.Contains(t, got, line)
			}
			// The daemon's planner must deliver exactly these instants.
			zone, err := time.LoadLocation(tc.zone)
			require.NoError(t, err)
			job := cron.JobDefinition{
				Version: 1, Kind: "job", Enabled: true, Overlap: "allow", Catchup: "all",
				Trigger: cron.Trigger{Kind: "cron", Cron: tc.cron, Timezone: tc.zone},
				Action:  cron.Action{Kind: "notify", Recipient: "current-owner-or-author"},
				Issue:   &cron.IssuePolicy{Kind: "per-run", ParentUID: nextParentUID, Title: "Review"},
			}
			batch, err := cron.PlanRecurring(job, "01ARZ3NDEKTSV4RRFFQ69G5FAW", progress, now.Add(72*time.Hour))
			require.NoError(t, err)
			require.GreaterOrEqual(t, len(batch.Due), 3)
			for i, due := range batch.Due[:3] {
				require.Equal(t, tc.want[i], due.In(zone).Format("2006-01-02 15:04 MST"))
			}
		})
	}
}

func TestCronJobDetailNativeIntervalSteps(t *testing.T) {
	anchor := time.Date(2026, 6, 1, 0, 30, 0, 0, time.UTC)
	progress := cron.RecurringProgress{Version: 1, Anchor: anchor, Through: anchor.Add(4 * time.Hour)}
	got := nextRunsDetail(t, "2026-06-01T05:00:00Z", nativeNotify(generated.TriggerResponse{Kind: "interval", IntervalSeconds: new(int64(7200))}), &progress)
	for _, line := range []string{"2026-06-01 06:30 UTC", "2026-06-01 08:30 UTC", "2026-06-01 10:30 UTC"} {
		require.Contains(t, got, line)
	}
}

func TestCronJobDetailNativeWithoutProgress(t *testing.T) {
	got := nextRunsDetail(t, "2026-06-01T05:00:00Z", nativeNotify(generated.TriggerResponse{Kind: "cron", Cron: new("0 7 * * *")}), nil)
	require.Regexp(t, `Next:\s+unavailable`, got)
}

func TestCronJobDetailAdapterTriggerTimes(t *testing.T) {
	got := nextRunsDetail(t, "2026-03-07T08:00:00Z", func(d *generated.CronJobDefinitionResponse) {
		d.Trigger = generated.TriggerResponse{Kind: "cron", Cron: new("30 2 * * *"), Timezone: new("America/New_York")}
	}, nil)
	require.Regexp(t, `Triggers:\s+2026-03-08 03:30 EDT`, got)
	require.Contains(t, got, "2026-03-10 02:30 EDT")
	require.NotContains(t, got, "Next:")

	got = nextRunsDetail(t, "2026-03-07T08:00:00Z", func(d *generated.CronJobDefinitionResponse) {
		d.Trigger = generated.TriggerResponse{Kind: "once", At: new("2026-04-01T12:00:00Z")}
	}, nil)
	require.Regexp(t, `Triggers:\s+2026-04-01 12:00 UTC`, got)

	got = nextRunsDetail(t, "2026-03-07T08:00:00Z", func(d *generated.CronJobDefinitionResponse) {
		d.Trigger = generated.TriggerResponse{Kind: "interval", IntervalSeconds: new(int64(3600))}
	}, nil)
	require.NotContains(t, got, "Triggers:")
	require.NotContains(t, got, "Next:")
}

func TestCronJobDetailPausedJobHasNoNextRuns(t *testing.T) {
	progress := cron.RecurringProgress{Version: 1, Anchor: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Through: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	native := nativeNotify(generated.TriggerResponse{Kind: "cron", Cron: new("0 7 * * *")})
	got := nextRunsDetail(t, "2026-06-01T05:00:00Z", func(d *generated.CronJobDefinitionResponse) {
		native(d)
		d.Enabled = false
	}, &progress)
	require.Regexp(t, `Next:\s+none while paused`, got)

	got = nextRunsDetail(t, "2026-06-01T05:00:00Z", func(d *generated.CronJobDefinitionResponse) { d.Enabled = false }, nil)
	require.Regexp(t, `Triggers:\s+none while paused`, got)
}

func TestCronScheduleNamesDeadlineLead(t *testing.T) {
	require.Equal(t, "issue-deadline, 1h0m0s before", cronSchedule(generated.TriggerResponse{Kind: "issue-deadline", LeadSeconds: new(int64(3600))}))
	require.Equal(t, "issue-deadline", cronSchedule(generated.TriggerResponse{Kind: "issue-deadline"}))
	require.NotContains(t, cronSchedule(generated.TriggerResponse{Kind: "issue-scheduled"}), "before")
}

type cronProgressTestAPI struct {
	cronReadTestAPI
	progress map[string]cron.RecurringProgress
	parents  []string
}

func (a *cronProgressTestAPI) ListCronProgress(_ context.Context, id int64, parent string) (map[string]cron.RecurringProgress, error) {
	a.parents = append(a.parents, parent)
	return a.progress, nil
}

func TestCronLoadReadsProgressOncePerParent(t *testing.T) {
	m := cronFixture(t)
	m.sseCh = nil
	native := nativeNotify(generated.TriggerResponse{Kind: "cron", Cron: new("0 7 * * *")})
	def := m.cron.data.jobs[0].Definition
	native(&def)
	first := m.cron.data.jobs[0]
	first.Definition = def
	second := first
	second.UID, second.Name = "01ARZ3NDEKTSV4RRFFQ69G5FBA", "Second"
	manual := first
	manual.UID, manual.Name = "01ARZ3NDEKTSV4RRFFQ69G5FBB", "Adapter"
	manual.Definition.Action = generated.ActionResponse{Kind: "execute", Prompt: new("go")}
	data := *m.cron.data
	data.jobs = []generated.CronJob{first, second, manual}
	progress := map[string]cron.RecurringProgress{first.UID: {Version: 1, Anchor: snapshotFixedNow, Through: snapshotFixedNow}}
	api := &cronProgressTestAPI{cronReadTestAPI: cronReadTestAPI{data: data}, progress: progress}
	m.api = api
	msg := m.fetchCron()()
	loaded, ok := msg.(cronLoadedMsg)
	require.True(t, ok)
	require.Equal(t, []string{nextParentUID}, api.parents, "one read per parent, none for adapter jobs")
	require.Equal(t, progress, loaded.progress)
}

func TestCronClientReadsNativeProgress(t *testing.T) {
	env := testenv.New(t)
	c := NewClient(env.URL, env.HTTP)
	ctx := t.Context()
	project, err := c.EnsureProject(ctx, "cron-progress-example", "worker")
	require.NoError(t, err)
	parent, err := c.CreateIssue(ctx, project.ID, CreateIssueBody{Title: "Recurring parent", Actor: "worker"})
	require.NoError(t, err)
	client, err := c.generatedClient()
	require.NoError(t, err)
	created, err := client.CreateCronJobWithResponse(ctx, &generated.CreateCronJobRequestOptions{
		PathParams: &generated.CreateCronJobPath{ProjectID: project.ID},
		Body: &generated.CreateCronJobBody{
			Actor: new("worker"), Name: "Review service",
			Definition: generated.CronJobDefinition{
				Version: 1, Kind: "job", Enabled: true,
				Trigger: generated.Trigger{Kind: "interval", IntervalSeconds: new(int64(3600))},
				Action:  generated.Action{Kind: "notify", Recipient: new("current-owner-or-author")},
				Issue:   &generated.IssuePolicy{Kind: "per-run", ParentUID: &parent.Issue.UID, Title: new("Review")},
				Overlap: "allow", Catchup: "all",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 201, created.HTTPResponse.StatusCode, string(created.Body))
	jobs, err := c.ListCronJobs(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	progress, err := c.ListCronProgress(ctx, project.ID, parent.Issue.UID)
	require.NoError(t, err)
	require.Contains(t, progress, jobs[0].UID)
	got := progress[jobs[0].UID]
	require.Equal(t, 1, got.Version)
	require.False(t, got.Anchor.IsZero())
	require.NoError(t, got.Validate())
}
