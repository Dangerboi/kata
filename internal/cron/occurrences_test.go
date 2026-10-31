package cron

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// delivered replays the daemon's planner with an all-catchup clock at until and
// returns every occurrence it would deliver after progress.Through.
func delivered(t *testing.T, j JobDefinition, p RecurringProgress, until time.Time) []time.Time {
	t.Helper()
	j.Catchup = "all"
	var out []time.Time
	for {
		batch, err := PlanRecurring(j, definitionUID, p, until)
		require.NoError(t, err)
		out = append(out, batch.Due...)
		p = batch.Progress
		if !batch.More {
			return out
		}
	}
}

func times(t *testing.T, values ...string) []time.Time {
	t.Helper()
	out := make([]time.Time, len(values))
	for i, v := range values {
		out[i] = at(t, v)
	}
	return out
}

func TestRecurringOccurrencesMatchDaemonDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, cron, zone, after string
		want                    []string
	}{
		{
			name: "spring forward gap runs an hour later", cron: "30 2 * * *", zone: "America/New_York", after: "2026-03-07T08:00:00Z",
			want: []string{"2026-03-08T07:30:00Z", "2026-03-09T06:30:00Z", "2026-03-10T06:30:00Z"},
		},
		{
			name: "fall back repeated hour runs twice", cron: "30 1 * * *", zone: "America/New_York", after: "2026-11-01T04:00:00Z",
			want: []string{"2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z", "2026-11-02T06:30:00Z"},
		},
		{
			name: "hourly through the repeat", cron: "0 * * * *", zone: "America/New_York", after: "2026-11-01T04:59:00Z",
			want: []string{"2026-11-01T05:00:00Z", "2026-11-01T06:00:00Z", "2026-11-01T07:00:00Z"},
		},
		{name: "seven is Sunday", cron: "0 9 * * 7", after: "2026-10-12T00:00:00Z", want: []string{"2026-10-18T09:00:00Z", "2026-10-25T09:00:00Z", "2026-11-01T09:00:00Z"}},
		{name: "wrapping range", cron: "0 9 * * 6-1", after: "2026-10-15T00:00:00Z", want: []string{"2026-10-17T09:00:00Z", "2026-10-18T09:00:00Z", "2026-10-19T09:00:00Z"}},
		{name: "five slash two is every other day from five", cron: "0 9 * * 5/2", after: "2026-10-12T00:00:00Z", want: []string{"2026-10-16T09:00:00Z", "2026-10-18T09:00:00Z", "2026-10-23T09:00:00Z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := nativeJob(t)
			j.Trigger = Trigger{Kind: "cron", Cron: tc.cron, Timezone: tc.zone}
			after := at(t, tc.after)
			p := RecurringProgress{Version: 1, Anchor: after, Through: after}
			got, err := RecurringOccurrences(j, p, after, 3)
			require.NoError(t, err)
			require.Equal(t, times(t, tc.want...), got)
			require.Equal(t, got, delivered(t, j, p, got[2])[:3], "daemon delivery must match the preview")
		})
	}
}

func TestRecurringOccurrencesLordHowe(t *testing.T) {
	// Lord Howe shifts by 30 minutes. Whatever go-cron does with the skipped
	// and repeated half hours, the preview must equal what the planner delivers.
	for _, start := range []string{"2026-04-03T00:00:00Z", "2026-10-02T00:00:00Z"} {
		for _, expr := range []string{"15 2 * * *", "45 1 * * *", "0 2 * * *"} {
			j := nativeJob(t)
			j.Trigger = Trigger{Kind: "cron", Cron: expr, Timezone: "Australia/Lord_Howe"}
			after := at(t, start)
			p := RecurringProgress{Version: 1, Anchor: after, Through: after}
			got, err := RecurringOccurrences(j, p, after, 5)
			require.NoError(t, err)
			require.Len(t, got, 5)
			require.Equal(t, got, delivered(t, j, p, got[4])[:5], "%s from %s", expr, start)
		}
	}
}

func TestRecurringOccurrencesNativeAnchor(t *testing.T) {
	j := nativeJob(t) // every 7200 seconds
	anchor := at(t, "2026-06-01T00:30:00Z")
	p := RecurringProgress{Version: 1, Anchor: anchor, Through: at(t, "2026-06-01T04:30:00Z")}
	now := at(t, "2026-06-01T05:00:00Z")
	got, err := RecurringOccurrences(j, p, now, 3)
	require.NoError(t, err)
	require.Equal(t, times(t, "2026-06-01T06:30:00Z", "2026-06-01T08:30:00Z", "2026-06-01T10:30:00Z"), got)
	require.Equal(t, got, delivered(t, j, p, got[2])[:3])
}

func TestRecurringOccurrencesSkipsNothingAlreadyDelivered(t *testing.T) {
	j := nativeJob(t)
	anchor := at(t, "2026-06-01T00:00:00Z")
	// Through is ahead of the preview clock: delivered occurrences are not previewed.
	p := RecurringProgress{Version: 1, Anchor: anchor, Through: at(t, "2026-06-01T06:00:00Z")}
	got, err := RecurringOccurrences(j, p, at(t, "2026-06-01T01:00:00Z"), 1)
	require.NoError(t, err)
	require.Equal(t, times(t, "2026-06-01T08:00:00Z"), got)
}

func TestRecurringOccurrencesPausedNative(t *testing.T) {
	j := nativeJob(t)
	j.Enabled = false
	anchor := at(t, "2026-06-01T00:00:00Z")
	p := RecurringProgress{Version: 1, Anchor: anchor, Through: anchor}
	got, err := RecurringOccurrences(j, p, anchor, 3)
	require.NoError(t, err)
	require.Empty(t, got)
	batch, err := PlanRecurring(j, definitionUID, p, anchor.Add(24*time.Hour))
	require.NoError(t, err)
	require.Empty(t, batch.Due, "the daemon delivers nothing for a paused job")
}

func adapterJob(t *testing.T, trigger Trigger) JobDefinition {
	t.Helper()
	j, err := ParseJob([]byte(executeJSON))
	require.NoError(t, err)
	j.Trigger = trigger
	j.Enabled = true
	return j
}

func TestRecurringOccurrencesAdapterTriggers(t *testing.T) {
	after := at(t, "2026-03-07T08:00:00Z")
	got, err := RecurringOccurrences(adapterJob(t, Trigger{Kind: "cron", Cron: "30 2 * * *", Timezone: "America/New_York"}), RecurringProgress{}, after, 3)
	require.NoError(t, err)
	require.Equal(t, times(t, "2026-03-08T07:30:00Z", "2026-03-09T06:30:00Z", "2026-03-10T06:30:00Z"), got)

	got, err = RecurringOccurrences(adapterJob(t, Trigger{Kind: "once", At: "2026-04-01T12:00:00+02:00"}), RecurringProgress{}, after, 3)
	require.NoError(t, err)
	require.Equal(t, times(t, "2026-04-01T10:00:00Z"), got)

	got, err = RecurringOccurrences(adapterJob(t, Trigger{Kind: "once", At: "2026-03-01T00:00:00Z"}), RecurringProgress{}, after, 3)
	require.NoError(t, err)
	require.Empty(t, got, "a past once trigger has no next time")

	// Kata holds no anchor, lease, or catch-up for an adapter interval.
	got, err = RecurringOccurrences(adapterJob(t, Trigger{Kind: "interval", IntervalSeconds: 3600}), RecurringProgress{}, after, 3)
	require.NoError(t, err)
	require.Empty(t, got)

	for _, trigger := range []Trigger{{Kind: "manual"}, {Kind: "issue-deadline", IssueUID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", LeadSeconds: 60}} {
		got, err = RecurringOccurrences(adapterJob(t, trigger), RecurringProgress{}, after, 3)
		require.NoError(t, err)
		require.Empty(t, got)
	}
}

func TestRecurringOccurrencesPausedAdapter(t *testing.T) {
	j := adapterJob(t, Trigger{Kind: "cron", Cron: "0 9 * * *"})
	j.Enabled = false
	got, err := RecurringOccurrences(j, RecurringProgress{}, at(t, "2026-03-07T08:00:00Z"), 3)
	require.NoError(t, err)
	require.Empty(t, got)
}
